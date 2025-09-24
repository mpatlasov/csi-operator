package generator

import (
	"fmt"
	"path/filepath"
	"strconv"

	generated_assets "github.com/openshift/csi-operator/pkg/generated-assets"
)

// AssetGenerator generates assets for CSI driver operators.
type AssetGenerator struct {
	flavour          ClusterFlavour
	operatorConfig   *CSIDriverGeneratorConfig
	replacements     []string
	controllerAssets map[string]*YAMLWithHistory
	guestAssets      map[string]*YAMLWithHistory
	reader           AssetReader
}

type AssetReader func(assetName string) ([]byte, error)

// NewAssetGenerator creates a new AssetGenerator.
func NewAssetGenerator(
	flavour ClusterFlavour,
	operatorConfig *CSIDriverGeneratorConfig,
	reader AssetReader) *AssetGenerator {
	return &AssetGenerator{
		flavour:        flavour,
		operatorConfig: operatorConfig,
		replacements: []string{
			"${ASSET_PREFIX}", operatorConfig.AssetPrefix,
			"${ASSET_SHORT_PREFIX}", operatorConfig.AssetShortPrefix,
			"${DRIVER_NAME}", operatorConfig.DriverName,
		},
		reader: reader,
	}
}

// GenerateAssets generates the assets for the CSI driver operator.
// No assets are saved to the filesystem, they are returned as a CSIDriverAssets struct.
func (gen *AssetGenerator) GenerateAssets() (*generated_assets.CSIDriverAssets, error) {
	if err := gen.generateController(); err != nil {
		return nil, err
	}
	if err := gen.generateGuest(); err != nil {
		return nil, err
	}
	return gen.collectAssets(), nil
}

func (gen *AssetGenerator) collectAssets() *generated_assets.CSIDriverAssets {
	generatedAssets := &generated_assets.CSIDriverAssets{
		ControllerAssets: make(map[string][]byte),
		GuestAssets:      make(map[string][]byte),
	}
	for name, a := range gen.controllerAssets {
		generatedAssets.ControllerAssets[name] = a.Render()
	}
	for name, a := range gen.guestAssets {
		generatedAssets.GuestAssets[name] = a.Render()
	}
	return generatedAssets
}

func (gen *AssetGenerator) generateController() error {
	gen.controllerAssets = make(map[string]*YAMLWithHistory)
	if err := gen.generateDeployment(); err != nil {
		return err
	}

	if err := gen.generateControllerMonitoringService(); err != nil {
		return err
	}

	if err := gen.collectControllerAssets(); err != nil {
		return err
	}

	if err := gen.patchController(); err != nil {
		return err
	}

	return nil
}

// Apply all controller patches in the generator config (CSIDriverGeneratorConfig.ControllerConfig.AssetPatches)
func (gen *AssetGenerator) patchController() error {
	for _, patch := range gen.operatorConfig.ControllerConfig.AssetPatches {
		if !patch.ClusterFlavours.Has(gen.flavour) {
			continue
		}
		assetYAML := gen.controllerAssets[patch.GeneratedAssetName]
		if assetYAML == nil {
			return fmt.Errorf("YAMLWithHistory %s not found to apply patch %s", patch.GeneratedAssetName, patch.PatchAssetName)
		}
		err := gen.applyAssetPatch(assetYAML, patch.PatchAssetName, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// Inject kube-rbac-proxy container for the metrics port into yamlFile. The yamlFile can be a Deployment or a
// DaemonSet. proxyPatchFile is the path to Deployment / DaemonSet patch file that adds the proxy container.
func (gen *AssetGenerator) addDriverRBACProxyContainers(yamlFile *YAMLWithHistory, proxyPatchFile string, localMetricsPort, exposedMetricsPort uint16, baseExtraReplacements []string) error {
	if localMetricsPort == 0 {
		return nil
	}

	extraReplacements := append([]string{}, baseExtraReplacements...) // Poor man's copy of the array.
	extraReplacements = append(extraReplacements,
		"${LOCAL_METRICS_PORT}", strconv.Itoa(int(localMetricsPort)),
		"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(exposedMetricsPort)),
	)
	err := gen.applyAssetPatch(yamlFile, proxyPatchFile, extraReplacements)
	if err != nil {
		return err
	}
	return nil
}

func (gen *AssetGenerator) generateDeployment() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	deploymentYAML := gen.mustReadBaseAsset("base/controller.yaml", nil)
	var err error

	err = gen.applyAssetPatch(deploymentYAML, ctrlCfg.DeploymentTemplateAssetName, nil)
	if err != nil {
		return err
	}

	localPortIndex := int(ctrlCfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(ctrlCfg.SidecarExposedMetricsPortStart)
	var baseExtraReplacements = []string{}
	if ctrlCfg.LivenessProbePort > 0 {
		baseExtraReplacements = append(baseExtraReplacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(ctrlCfg.LivenessProbePort)))
	}

	err = gen.addDriverRBACProxyContainers(deploymentYAML, "common/sidecars/controller_driver_kube_rbac_proxy.yaml", ctrlCfg.LocalMetricsPort, ctrlCfg.ExposedMetricsPort, baseExtraReplacements)
	if err != nil {
		return err
	}

	// Inject sidecars and their kube-rbac-proxies.
	for i := 0; i < len(ctrlCfg.Sidecars); i++ {
		sidecar := ctrlCfg.Sidecars[i]
		extraReplacements := append([]string{}, baseExtraReplacements...)
		if sidecar.HasMetricsPort {
			extraReplacements = append(extraReplacements,
				"${LOCAL_METRICS_PORT}", strconv.Itoa(localPortIndex),
				"${EXPOSED_METRICS_PORT}", strconv.Itoa(exposedPortIndex),
				"${PORT_NAME}", sidecar.MetricPortName,
			)
			localPortIndex++
			exposedPortIndex++
		}
		err = gen.addSidecar(deploymentYAML, sidecar.TemplateAssetName, extraReplacements, sidecar.ExtraArguments, gen.flavour, sidecar.AssetPatches)
		if err != nil {
			return err
		}
	}
	gen.controllerAssets[generated_assets.ControllerDeploymentAssetName] = deploymentYAML
	return nil
}

// Add driver's MetricsPort to the metrics Service and ServiceMonitor.
func (gen *AssetGenerator) generateDriverMetricsService(serviceYAML, serviceMonitorYAML *YAMLWithHistory, localMetricsPort, exposedMetricsPort uint16, servicePrefix string) error {
	if localMetricsPort == 0 {
		return nil
	}

	extraReplacements := []string{
		"${LOCAL_METRICS_PORT}", strconv.Itoa(int(localMetricsPort)),
		"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(exposedMetricsPort)),
		"${PORT_NAME}", "driver-m",
		"${SERVICE_PREFIX}", servicePrefix,
	}
	var err error
	err = gen.applyAssetPatch(serviceYAML, "common/metrics/service_add_port.yaml", extraReplacements)
	if err != nil {
		return err
	}
	err = gen.applyAssetPatch(serviceMonitorYAML, "common/metrics/service_monitor_add_port.yaml.patch", extraReplacements)
	if err != nil {
		return err
	}
	return nil
}

func (gen *AssetGenerator) generateSidecarMetricsServices(serviceYAML, serviceMonitorYAML *YAMLWithHistory, localPortStart, exposedPortStart int, sidecars []SidecarConfig, servicePerfix string) error {
	localPortIndex := localPortStart
	exposedPortIndex := exposedPortStart

	for i := 0; i < len(sidecars); i++ {
		sidecar := sidecars[i]
		if !sidecar.HasMetricsPort {
			continue
		}
		extraReplacements := []string{
			"${LOCAL_METRICS_PORT}", strconv.Itoa(localPortIndex),
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(exposedPortIndex),
			"${PORT_NAME}", sidecar.MetricPortName,
			"${SERVICE_PREFIX}", servicePerfix,
		}
		localPortIndex++
		exposedPortIndex++

		var err error
		err = gen.applyAssetPatch(serviceYAML, "common/metrics/service_add_port.yaml", extraReplacements)
		if err != nil {
			return err
		}
		err = gen.applyAssetPatch(serviceMonitorYAML, "common/metrics/service_monitor_add_port.yaml.patch", extraReplacements)
		if err != nil {
			return err
		}
	}
	return nil
}

func (gen *AssetGenerator) generateControllerMonitoringService() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	serviceYAML := gen.mustReadBaseAsset("base/controller_metrics_service.yaml", nil)
	serviceMonitorYAML := gen.mustReadBaseAsset("base/controller_metrics_servicemonitor.yaml", nil)

	if err := gen.generateSidecarMetricsServices(serviceYAML, serviceMonitorYAML, int(ctrlCfg.SidecarLocalMetricsPortStart), int(ctrlCfg.SidecarExposedMetricsPortStart), ctrlCfg.Sidecars, "controller"); err != nil {
		return err
	}
	if err := gen.generateDriverMetricsService(serviceYAML, serviceMonitorYAML, ctrlCfg.LocalMetricsPort, ctrlCfg.ExposedMetricsPort, "controller"); err != nil {
		return err
	}

	gen.controllerAssets[generated_assets.ControllerMetricServiceAssetName] = serviceYAML
	if gen.flavour != FlavourHyperShift {
		// TODO: figure out monitoring on HyperShift. The operator does not have RBAC for ServiceMonitors now.
		gen.controllerAssets[generated_assets.ControllerMetricServiceMonitorAssetName] = serviceMonitorYAML
	}
	return nil
}

func (gen *AssetGenerator) collectControllerAssets() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	for _, a := range ctrlCfg.Assets {
		if a.ClusterFlavours.Has(gen.flavour) {
			assetBytes := gen.mustReadBaseAsset(a.AssetName, nil)
			gen.controllerAssets[filepath.Base(a.AssetName)] = assetBytes
		}
	}
	return nil
}

func (gen *AssetGenerator) generateGuest() error {
	gen.guestAssets = make(map[string]*YAMLWithHistory)

	if err := gen.generateDaemonSet(); err != nil {
		return err
	}
	if err := gen.generateGuestMonitoringService(); err != nil {
		return err
	}
	if err := gen.collectGuestAssets(); err != nil {
		return err
	}
	if err := gen.patchGuest(); err != nil {
		return err
	}
	return nil
}

func (gen *AssetGenerator) generateDaemonSet() error {
	cfg := gen.operatorConfig.GuestConfig
	dsYAML := gen.mustReadBaseAsset("base/node.yaml", nil)
	var err error

	extraReplacements := []string{}
	if cfg.LivenessProbePort > 0 {
		extraReplacements = append(extraReplacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(cfg.LivenessProbePort)))
	}

	if cfg.NodeRegistrarHealthCheckPort > 0 {
		extraReplacements = append(extraReplacements, "${NODE_DRIVER_REGISTRAR_HEALTH_PORT}", strconv.Itoa(int(cfg.NodeRegistrarHealthCheckPort)))
	}

	err = gen.applyAssetPatch(dsYAML, cfg.DaemonSetTemplateAssetName, extraReplacements)
	if err != nil {
		return err
	}

	err = gen.addDriverRBACProxyContainers(dsYAML, "common/sidecars/node_driver_kube_rbac_proxy.yaml", cfg.LocalMetricsPort, cfg.ExposedMetricsPort, extraReplacements)
	if err != nil {
		return err
	}

	for i := 0; i < len(cfg.Sidecars); i++ {
		sidecar := cfg.Sidecars[i]
		err = gen.addSidecar(dsYAML, sidecar.TemplateAssetName, extraReplacements, sidecar.ExtraArguments, gen.flavour, sidecar.AssetPatches)
		if err != nil {
			return err
		}
	}
	gen.guestAssets[generated_assets.NodeDaemonSetAssetName] = dsYAML
	return nil
}

func (gen *AssetGenerator) generateGuestMonitoringService() error {
	cfg := gen.operatorConfig.GuestConfig

	if cfg.LocalMetricsPort == 0 {
		// Do not add metrics service if driver does not expose any metrics.
		// There is no node-level sidecar that would export one.
		return nil
	}
	serviceYAML := gen.mustReadBaseAsset("base/node_metrics_service.yaml", nil)
	serviceMonitorYAML := gen.mustReadBaseAsset("base/node_metrics_servicemonitor.yaml", nil)

	err := gen.generateDriverMetricsService(serviceYAML, serviceMonitorYAML, cfg.LocalMetricsPort, cfg.ExposedMetricsPort, "node")
	if err != nil {
		return err
	}

	gen.guestAssets[generated_assets.NodeMetricServiceAssetName] = serviceYAML
	gen.guestAssets[generated_assets.NodeMetricServiceMonitorAssetName] = serviceMonitorYAML

	// Add metrics RBACs for node service monitor
	gen.guestAssets["node_kube_rbac_proxy_role.yaml"] = gen.mustReadBaseAsset("base/rbac/node_kube_rbac_proxy_role.yaml", nil)
	gen.guestAssets["node_kube_rbac_proxy_binding.yaml"] = gen.mustReadBaseAsset("base/rbac/node_kube_rbac_proxy_binding.yaml", nil)

	return nil
}

// Apply all patches in the generator config (CSIDriverGeneratorConfig.GuestConfig.AssetPatches)
func (gen *AssetGenerator) patchGuest() error {
	// Patch everything, including the CSI driver DaemonSet.
	for _, patch := range gen.operatorConfig.GuestConfig.AssetPatches {
		if !patch.ClusterFlavours.Has(gen.flavour) {
			continue
		}
		assetYAML := gen.guestAssets[patch.GeneratedAssetName]
		if assetYAML == nil {
			return fmt.Errorf("YAMLWithHistory %s not found to apply patch %s", patch.GeneratedAssetName, patch.PatchAssetName)
		}

		err := gen.applyAssetPatch(assetYAML, patch.PatchAssetName, nil)
		if err != nil {
			return err
		}
		gen.guestAssets[patch.GeneratedAssetName] = assetYAML
	}
	return nil
}

func (gen *AssetGenerator) collectGuestAssets() error {
	cfg := gen.operatorConfig.GuestConfig
	for _, a := range cfg.Assets {
		if a.ClusterFlavours.Has(gen.flavour) {
			assetBytes := gen.mustReadBaseAsset(a.AssetName, nil)
			gen.guestAssets[filepath.Base(a.AssetName)] = assetBytes
		}
	}

	// Collect all guest static assets from the controller config too - e.g. sidecar RBAC rules need to be present in
	// the guest cluster.
	ctrlCfg := gen.operatorConfig.ControllerConfig
	for _, sidecar := range ctrlCfg.Sidecars {
		for _, assetName := range sidecar.GuestAssetNames {
			assetBytes := gen.mustReadBaseAsset(assetName, nil)
			assetBytes.Logf("  because it's needed by controller sidecar %s", sidecar.TemplateAssetName)
			gen.guestAssets[filepath.Base(assetName)] = assetBytes
		}
	}

	return nil
}
