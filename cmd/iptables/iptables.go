package main

import (
	"k8s.io/kubernetes/pkg/util/iptables"
	"k8s.io/utils/exec"
)

func main() {
	execer := exec.New()

	iptablesClient := iptables.New(execer, iptables.ProtocolIpv4)

	iptablesClient.EnsureRule(iptables.Prepend, "nat", "KUBE-POSTROUTING", "-s", "1.1.1.1/32", "-j", "MASQUERADE")
	iptablesClient.EnsureRule(iptables.Prepend, "nat", "KUBE-POSTROUTING", "-s", "1.1.1.2/32", "-j", "MASQUERADE")
}
