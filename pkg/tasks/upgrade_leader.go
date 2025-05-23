/*
Copyright 2019 The KubeOne Authors.

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

package tasks

import (
	"time"

	kubeoneapi "k8c.io/kubeone/pkg/apis/kubeone"
	"k8c.io/kubeone/pkg/executor"
	"k8c.io/kubeone/pkg/nodeutils"
	"k8c.io/kubeone/pkg/scripts"
	"k8c.io/kubeone/pkg/state"
)

func upgradeLeader(s *state.State) error {
	return s.RunTaskOnLeader(upgradeLeaderExecutor)
}

func upgradeLeaderExecutor(s *state.State, node *kubeoneapi.HostConfig, conn executor.Interface) error {
	logger := s.Logger.WithField("node", node.PublicAddress)

	logger.Infoln("Labeling leader control plane...")
	if err := labelNode(s.DynamicClient, node); err != nil {
		return err
	}

	drainer := nodeutils.NewDrainer(s.RESTConfig, logger)

	logger.Infoln("Cordoning leader control plane...")
	if err := drainer.Cordon(s.Context, node.Hostname, true); err != nil {
		return err
	}

	logger.Infoln("Draining leader control plane...")
	if err := drainer.Drain(s.Context, node.Hostname); err != nil {
		return err
	}

	if err := setupProxy(logger, s); err != nil {
		return err
	}

	if err := updateKubeadmFlagsEnv(s, node); err != nil {
		return err
	}

	logger.Infoln("Upgrading kubeadm binary on the leader control plane...")
	if err := upgradeKubernetesBinaries(s, *node, scripts.Params{Kubeadm: true, Kubectl: true}); err != nil {
		return err
	}

	logger.Infoln("Running 'kubeadm upgrade' on leader control plane node...")
	if err := upgradeLeaderControlPlane(s, node.ID); err != nil {
		return err
	}

	logger.Infoln("Uncordoning leader control plane...")
	if err := drainer.Cordon(s.Context, node.Hostname, false); err != nil {
		return err
	}

	logger.Infof("Waiting %v to ensure all components are up...", timeoutNodeUpgrade)
	time.Sleep(timeoutNodeUpgrade)

	logger.Infoln("Unlabeling leader control plane...")
	if err := unlabelNode(s.DynamicClient, node); err != nil {
		return err
	}

	return approvePendingCSR(s, node, conn)
}
