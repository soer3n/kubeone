/*
Copyright 2021 The KubeOne Authors.

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

package scripts

import (
	kubeoneapi "k8c.io/kubeone/pkg/apis/kubeone"
	"k8c.io/kubeone/pkg/containerruntime"
	"k8c.io/kubeone/pkg/fail"
)

const (
	kubeadmDebianTemplate = `
sudo swapoff -a
sudo sed -i '/.*swap.*/d' /etc/fstab
sudo systemctl disable --now ufw || true

source /etc/kubeone/proxy-env

{{ template "sysctl-k8s" . }}
{{ template "journald-config" }}

sudo mkdir -p /etc/apt/apt.conf.d
cat <<EOF | sudo tee /etc/apt/apt.conf.d/proxy.conf
{{- if .HTTPS_PROXY }}
Acquire::https::Proxy "{{ .HTTPS_PROXY }}";
{{- end }}
{{- if .HTTP_PROXY }}
Acquire::http::Proxy "{{ .HTTP_PROXY }}";
{{- end }}
EOF

# Removing deprecated Kubernetes repositories from apt sources is needed when upgrading from older KubeOne versions,
# otherwise, apt-get update will fail to upgrade the packages.
{{- if .CONFIGURE_REPOSITORIES }}
if sudo grep -q "deb http://apt.kubernetes.io/ kubernetes-xenial main" /etc/apt/sources.list.d/kubernetes.list; then
  rm -f /etc/apt/sources.list.d/kubernetes.list
fi

sudo install -m 0755 -d /etc/apt/keyrings
LATEST_STABLE=$(curl -sL https://dl.k8s.io/release/stable.txt | sed 's/\.[0-9]*$//')
curl -fsSL https://pkgs.k8s.io/core:/stable:/${LATEST_STABLE}/deb/Release.key | sudo gpg --dearmor --yes -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/{{ .KUBERNETES_MAJOR_MINOR }}/deb/ /" | sudo tee /etc/apt/sources.list.d/kubernetes.list
{{- end }}

sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install --option "Dpkg::Options::=--force-confold" -y --no-install-recommends \
	apt-transport-https \
	ca-certificates \
	curl \
	gnupg \
	apparmor-utils \
	lsb-release \
	{{- if .INSTALL_ISCSI_AND_NFS }}
	open-iscsi \
	nfs-common \
	{{- end }}
	rsync

{{- if .INSTALL_ISCSI_AND_NFS }}
sudo systemctl enable --now iscsid
{{- end }}

kube_ver="{{ .KUBERNETES_VERSION }}-*"

{{- if or .FORCE .UPGRADE }}
sudo apt-mark unhold kubelet kubeadm kubectl kubernetes-cni cri-tools
{{- end }}

{{ if .INSTALL_CONTAINERD }}
{{ template "apt-containerd" . }}
{{ end }}

sudo DEBIAN_FRONTEND=noninteractive apt-get install \
	--option "Dpkg::Options::=--force-confold" \
	--no-install-recommends \
	{{- if .FORCE }}
	--allow-downgrades \
	{{- end }}
	-y \
{{- if .KUBELET }}
	kubelet=${kube_ver} \
{{- end }}
{{- if .KUBEADM }}
	kubeadm=${kube_ver} \
{{- end }}
{{- if .KUBECTL }}
	kubectl=${kube_ver} \
{{- end }}
	kubernetes-cni \
	cri-tools

sudo apt-mark hold kubelet kubeadm kubectl kubernetes-cni cri-tools

sudo systemctl daemon-reload
sudo systemctl enable --now kubelet

{{- if or .FORCE .KUBELET }}
sudo systemctl restart kubelet
{{- end }}
`

	removeBinariesDebianScriptTemplate = `
sudo apt-mark unhold kubelet kubeadm kubectl kubernetes-cni cri-tools
sudo apt-get remove --purge -y \
	kubeadm \
	kubectl \
	kubelet
sudo apt-get remove --purge -y kubernetes-cni cri-tools || true
sudo rm -rf /opt/cni
sudo rm -f /etc/systemd/system/kubelet.service /etc/systemd/system/kubelet.service.d/10-kubeadm.conf
sudo systemctl daemon-reload
`
)

func DebScript(cluster *kubeoneapi.KubeOneCluster, params Params) (string, error) {
	data := Data{
		"UPGRADE":                params.Upgrade,
		"KUBELET":                params.Kubelet,
		"KUBECTL":                params.Kubectl,
		"KUBEADM":                params.Kubeadm,
		"FORCE":                  params.Force,
		"KUBERNETES_VERSION":     cluster.Versions.Kubernetes,
		"KUBERNETES_MAJOR_MINOR": cluster.Versions.KubernetesMajorMinorVersion(),
		"CONFIGURE_REPOSITORIES": cluster.SystemPackages.ConfigureRepositories,
		"HTTP_PROXY":             cluster.Proxy.HTTP,
		"HTTPS_PROXY":            cluster.Proxy.HTTPS,
		"INSTALL_CONTAINERD":     cluster.ContainerRuntime.Containerd,
		"INSTALL_ISCSI_AND_NFS":  installISCSIAndNFS(cluster),
		"IPV6_ENABLED":           cluster.ClusterNetwork.HasIPv6(),
	}

	if err := containerruntime.UpdateDataMap(cluster, data); err != nil {
		return "", err
	}

	result, err := Render(kubeadmDebianTemplate, data)

	return result, fail.Runtime(err, "rendering kubeadmDebianTemplate script")
}

func RemoveBinariesDebian() (string, error) {
	result, err := Render(removeBinariesDebianScriptTemplate, Data{})

	return result, fail.Runtime(err, "rendering removeBinariesDebianScriptTemplate script")
}
