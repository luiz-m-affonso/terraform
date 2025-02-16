// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package stackplan

import (
	"fmt"
	"time"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/states"
)

// Component is a container for a set of changes that all belong to the same
// component instance as declared in a stack configuration.
//
// Each instance of component essentially maps to one call into the main
// Terraform language runtime to apply all of the described changes together as
// a single operation.
type Component struct {
	// ResourceInstancePlanned describes the changes that Terraform is proposing
	// to make to try to converge the real system state with the desired state
	// as described by the configuration.
	ResourceInstancePlanned addrs.Map[addrs.AbsResourceInstanceObject, *plans.ResourceInstanceChangeSrc]

	// ResourceInstancePriorState describes the state as it was when making
	// the proposals described in [Component.ResourceInstancePlanned].
	//
	// Elements of this map have nil values if the planned action is "create",
	// since in that case there is no prior object.
	ResourceInstancePriorState addrs.Map[addrs.AbsResourceInstanceObject, *states.ResourceInstanceObjectSrc]

	// ResourceInstanceProviderConfig is a lookup table from resource instance
	// object address to the address of the provider configuration that
	// will handle any apply-time actions for that object.
	ResourceInstanceProviderConfig addrs.Map[addrs.AbsResourceInstanceObject, addrs.AbsProviderConfig]

	// TODO: Something for deferred resource instance changes, once we have
	// such a concept.

	// PlanTimestamp is the time Terraform Core recorded as the single "plan
	// timestamp", which is used only for the result of the "plantimestamp"
	// function during apply and must not be used for any other purpose.
	PlanTimestamp time.Time
}

// ForModulesRuntime translates the component instance plan into the form
// expected by the modules runtime, which is what would ultimately be used
// to apply the plan.
//
// The stack component planning model preserves only the most crucial details
// of a component plan produced by the modules runtime, and so the result
// will not exactly match the [plans.Plan] that the component plan was produced
// from, but should be complete enough to successfully apply the plan.
//
// Conversion with this method should always succeed if the given previous
// run state is truly the one that the plan was created from. If this method
// returns an error then that suggests that the recieving plan is inconsistent
// with the given previous run state, which should not happen if the caller
// is using Terraform Core correctly.
func (c *Component) ForModulesRuntime() (*plans.Plan, error) {
	changes := plans.NewChanges()
	plan := &plans.Plan{
		Changes:   changes,
		Timestamp: c.PlanTimestamp,
	}

	sc := changes.SyncWrapper()
	for _, elem := range c.ResourceInstancePlanned.Elems {
		changeSrc := elem.Value
		if changeSrc != nil {
			sc.AppendResourceInstanceChange(changeSrc)
		}
	}

	priorState := states.NewState()
	ss := priorState.SyncWrapper()
	for _, elem := range c.ResourceInstancePriorState.Elems {
		addr := elem.Key
		providerConfigAddr, ok := c.ResourceInstanceProviderConfig.GetOk(addr)
		if !ok {
			return nil, fmt.Errorf("no provider config address for %s", addr)
		}
		stateSrc := elem.Value
		if addr.IsCurrent() {
			ss.SetResourceInstanceCurrent(addr.ResourceInstance, stateSrc, providerConfigAddr)
		} else {
			ss.SetResourceInstanceDeposed(addr.ResourceInstance, addr.DeposedKey, stateSrc, providerConfigAddr)
		}
	}

	plan.PriorState = priorState
	plan.PrevRunState = priorState.DeepCopy() // This is just here to complete the data structure; we don't really do anything with it

	return plan, nil
}
