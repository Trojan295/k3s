package egresssnat

import (
	"context"
	"time"

	"github.com/rancher/k3s/pkg/daemons/config"
	api "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/iptables"
	"k8s.io/utils/exec"
)

func Run(ctx context.Context, nodeConfig *config.Node) error {
	restConfig, err := clientcmd.BuildConfigFromFlags("", nodeConfig.AgentConfig.KubeConfigK3sController)
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	esc, err := NewController(ctx.Done(), client, time.Minute)
	if err != nil {
		return err
	}

	go esc.Run(ctx.Done())

	return nil
}

type Controller struct {
	podLister cache.Indexer
	npLister  cache.Indexer

	PodEventHandler       cache.ResourceEventHandler
	NamespaceEventHandler cache.ResourceEventHandler

	syncPeriod time.Duration
}

func (esc *Controller) Run(stopCh <-chan struct{}) {
	t := time.NewTicker(esc.syncPeriod)
	defer t.Stop()

	log.Info("Starting egress SNAT controller")

	// loop forever till notified to stop on stopCh
	for {
		select {
		case <-stopCh:
			log.Info("Shutting down egress SNAT controller")
			return
		default:
		}

		//log.V(1).Info("Performing periodic sync of iptables to reflect network policies")
		//err := npc.Sync()
		//if err != nil {
		//	log.Errorf("Error during periodic sync of network policies in network policy controller. Error: " + err.Error())
		//	log.Errorf("Skipping sending heartbeat from network policy controller as periodic sync failed.")
		//}

		select {
		case <-stopCh:
			log.Infof("Shutting down egress SNAT controller")
			return
		case <-t.C:
		}
	}
}

func (esc *Controller) onPodDelete(obj interface{}) {
	pod := obj.(*api.Pod)

	snatIPAddress, present := pod.Annotations["egressSNATIpAddress"]
	if !present {
		return
	}

	log.V(2).Infof("Received update to pod: %s/%s", pod.Namespace, pod.Name)

	podIP := pod.Status.PodIP

	execer := exec.New()
	iptablesClient := iptables.New(execer, iptables.ProtocolIpv4)
	iptablesClient.DeleteRule("nat", "KUBE-POSTROUTING", "-s", podIP, "-j", "SNAT", "--to-source", snatIPAddress)
}

func (esc *Controller) onPodUpdate(obj interface{}) {
	pod := obj.(*api.Pod)

	snatIPAddress, present := pod.Annotations["egressSNATIpAddress"]
	if !present {
		return
	}

	log.V(2).Infof("Received update to pod: %s/%s", pod.Namespace, pod.Name)

	podIP := pod.Status.PodIP

	execer := exec.New()
	iptablesClient := iptables.New(execer, iptables.ProtocolIpv4)
	iptablesClient.EnsureRule(iptables.Prepend, "nat", "KUBE-POSTROUTING", "-s", podIP, "-j", "SNAT", "--to-source", snatIPAddress)
}

func (esc *Controller) newPodEventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			esc.onPodUpdate(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newPoObj := newObj.(*api.Pod)
			oldPoObj := oldObj.(*api.Pod)
			if newPoObj.Status.PodIP != oldPoObj.Status.PodIP {
				esc.onPodUpdate(newObj)
			}
		},
		DeleteFunc: func(obj interface{}) {
			esc.onPodDelete(obj)
		},
	}
}

func NewController(
	stopCh <-chan struct{},
	clientset kubernetes.Interface,
	ipTablesSyncPeriod time.Duration) (*Controller, error) {

	esc := &Controller{}
	esc.syncPeriod = ipTablesSyncPeriod

	informerFactory := informers.NewSharedInformerFactory(clientset, 0)
	podInformer := informerFactory.Core().V1().Pods().Informer()
	informerFactory.Start(stopCh)

	esc.podLister = podInformer.GetIndexer()
	esc.PodEventHandler = esc.newPodEventHandler()
	podInformer.AddEventHandler(esc.PodEventHandler)

	return esc, nil
}
