package machine

// Copyright (c) Microsoft Corporation.
// Licensed under the Apache License 2.0.

import (
	"context"
	"fmt"

	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Azure/ARO-RP/pkg/api"
	"github.com/Azure/ARO-RP/pkg/api/validate"
	utilmachine "github.com/Azure/ARO-RP/pkg/util/machine"
	_ "github.com/Azure/ARO-RP/pkg/util/scheme"
)

func (r *Reconciler) workerReplicas(ctx context.Context) (int, error) {
	count := 0
	machinesets, err := r.maocli.MachineV1beta1().MachineSets(machineSetsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	// Count MachineSets using Spec.Replicas
	for _, machineset := range machinesets.Items {
		if machineset.Spec.Replicas != nil {
			count += int(*machineset.Spec.Replicas)
		}
	}
	return count, nil
}

func (r *Reconciler) machineValid(ctx context.Context, machine *machinev1beta1.Machine, isMaster bool) (errs []error) {
	// Validate machine provider spec exists and decode it
	if machine.Spec.ProviderSpec.Value == nil {
		return []error{fmt.Errorf("machine %s: provider spec missing", machine.Name)}
	}

	machineProviderSpec, err := utilmachine.UnmarshalAzureProviderSpec(machine.Name, utilmachine.Machine, machine.Spec.ProviderSpec.Value.Raw)
	if err != nil {
		errs = append(errs, err)
	}

	// Validate VM size in machine provider spec
	if !validate.VMSizeIsValid(api.VMSize(machineProviderSpec.VMSize), r.isLocalDevelopmentMode, isMaster) {
		errs = append(errs, fmt.Errorf("machine %s: invalid VM size '%v'", machine.Name, machineProviderSpec.VMSize))
	}

	// Validate disk size in machine provider spec
	if !isMaster && !validate.DiskSizeIsValid(int(machineProviderSpec.OSDisk.DiskSizeGB)) {
		errs = append(errs, fmt.Errorf("machine %s: invalid disk size '%v'", machine.Name, machineProviderSpec.OSDisk.DiskSizeGB))
	}

	// Validate image publisher and offer
	if machineProviderSpec.Image.Publisher != "azureopenshift" || machineProviderSpec.Image.Offer != "aro4" {
		errs = append(errs, fmt.Errorf("machine %s: invalid image '%v'", machine.Name, machineProviderSpec.Image))
	}

	if machineProviderSpec.ManagedIdentity != "" {
		errs = append(errs, fmt.Errorf("machine %s: invalid managedIdentity '%v'", machine.Name, machineProviderSpec.ManagedIdentity))
	}

	return errs
}

func (r *Reconciler) checkMachines(ctx context.Context) (errs []error) {
	actualWorkers := 0
	actualMasters := 0

	expectedMasters := 3
	expectedWorkers, err := r.workerReplicas(ctx)
	if err != nil {
		return []error{err}
	}

	machines, err := r.maocli.MachineV1beta1().Machines(machineSetsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return []error{err}
	}

	for _, machine := range machines.Items {
		isMaster, err := isMasterRole(&machine)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		errs = append(errs, r.machineValid(ctx, &machine, isMaster)...)

		if isMaster {
			actualMasters++
		} else {
			actualWorkers++
		}
	}

	if actualMasters != expectedMasters {
		errs = append(errs, fmt.Errorf("invalid number of master machines %d, expected %d", actualMasters, expectedMasters))
	}

	if actualWorkers != expectedWorkers {
		errs = append(errs, fmt.Errorf("invalid number of worker machines %d, expected %d", actualWorkers, expectedWorkers))
	}

	return errs
}

func isMasterRole(m *machinev1beta1.Machine) (bool, error) {
	role, ok := m.Labels["machine.openshift.io/cluster-api-machine-role"]
	if !ok {
		return false, fmt.Errorf("machine %s: cluster-api-machine-role label not found", m.Name)
	}
	return role == "master", nil
}
