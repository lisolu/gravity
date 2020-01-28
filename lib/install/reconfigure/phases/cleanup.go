/*
Copyright 2020 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package phases

import (
	"strings"

	"github.com/gravitational/gravity/lib/fsm"
	"github.com/gravitational/gravity/lib/ops"
	"github.com/gravitational/gravity/lib/pack"
	"github.com/gravitational/gravity/lib/storage"

	"github.com/gravitational/trace"
	"k8s.io/client-go/kubernetes"
)

// NewCleanup dispatches the specific cleanup phase to an appropriate executor.
func NewCleanup(p fsm.ExecutorParams, operator ops.Operator, packages pack.PackageService, client *kubernetes.Clientset) (fsm.PhaseExecutor, error) {
	switch phaseID := strings.Replace(p.Phase.ID, CleanupPhase, "", 1); phaseID {
	case PackagesPhase:
		return NewPackages(p, operator, packages)
	case StatePhase:
		return NewState(p, operator)
	case NetworkPhase:
		return NewNetwork(p, operator)
	case TokensPhase:
		return NewTokens(p, operator, client)
	case NodePhase:
		return NewNode(p, operator, client)
	}
	return nil, trace.BadParameter("unknown phase %q", p.Phase.ID)
}

func opKey(plan storage.OperationPlan) ops.SiteOperationKey {
	return ops.SiteOperationKey{
		AccountID:   plan.AccountID,
		SiteDomain:  plan.ClusterName,
		OperationID: plan.OperationID,
	}
}

const (
	// CleanupPhase does post IP change cleanups.
	//
	// The phases defined below are its sub-phases.
	CleanupPhase = "/cleanup"
	// PackagesPhase removes old configuration and secret packages.
	PackagesPhase = "/packages"
	// StatePhase updates the cluster state.
	StatePhase = "/state"
	// NetworkPhase removes old network interfaces.
	NetworkPhase = "/interfaces"
	// TokensPhase removes old service account tokens.
	TokensPhase = "/tokens"
	// NodePhase removes old Kubernetes node object.
	NodePhase = "/node"
)
