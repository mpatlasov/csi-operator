// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

// NodeDisruptionPolicySpecFileApplyConfiguration represents an declarative configuration of the NodeDisruptionPolicySpecFile type for use
// with apply.
type NodeDisruptionPolicySpecFileApplyConfiguration struct {
	Path    *string                                            `json:"path,omitempty"`
	Actions []NodeDisruptionPolicySpecActionApplyConfiguration `json:"actions,omitempty"`
}

// NodeDisruptionPolicySpecFileApplyConfiguration constructs an declarative configuration of the NodeDisruptionPolicySpecFile type for use with
// apply.
func NodeDisruptionPolicySpecFile() *NodeDisruptionPolicySpecFileApplyConfiguration {
	return &NodeDisruptionPolicySpecFileApplyConfiguration{}
}

// WithPath sets the Path field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Path field is set to the value of the last call.
func (b *NodeDisruptionPolicySpecFileApplyConfiguration) WithPath(value string) *NodeDisruptionPolicySpecFileApplyConfiguration {
	b.Path = &value
	return b
}

// WithActions adds the given value to the Actions field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Actions field.
func (b *NodeDisruptionPolicySpecFileApplyConfiguration) WithActions(values ...*NodeDisruptionPolicySpecActionApplyConfiguration) *NodeDisruptionPolicySpecFileApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithActions")
		}
		b.Actions = append(b.Actions, *values[i])
	}
	return b
}