package kubevirt

import (
	"context"
	"sync"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/cldmnky/oooi/internal/dhcp/plugins/kubevirt/client/versioned"
)

var log = logger.GetLogger("plugins/kubevirt")

var Plugin = plugins.Plugin{
	Name:   "kubevirt",
	Setup4: setupKubevirt,
}

type KubevirtInstance struct {
	Name       string
	Namespace  string
	Interfaces []kubevirtv1.VirtualMachineInstanceNetworkInterface
}

type KubevirtState struct {
	sync.Mutex
	Client    versioned.Interface
	Instances []KubevirtInstance
}

func setupKubevirt(args ...string) (handler.Handler4, error) {
	var (
		k   KubevirtState
		err error
		cfg *rest.Config
	)
	k.Lock()
	defer k.Unlock()
	if len(args) == 0 {
		cfg, err = clientcmd.BuildConfigFromFlags("", "")
		if err != nil {
			log.WithError(err).Error("failed to build kubeconfig")
			return nil, err
		}
	} else {

		cfg, err = clientcmd.BuildConfigFromFlags("", args[0])
		if err != nil {
			log.WithError(err).Error("failed to build kubeconfig")
			return nil, err
		}
	}
	k.Client, err = versioned.NewForConfig(cfg)
	if err != nil {
		log.WithError(err).Error("failed to create kubevirt client")
		return nil, err
	}
	return k.kubevirtHandler4, nil
}

func (k *KubevirtState) kubevirtHandler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	k.Lock()
	defer k.Unlock()
	// refresh instances
	if err := k.refreshKubevirtInstances(); err != nil {
		log.WithError(err).Error("failed to refresh kubevirt instances")
		return nil, true
	}
	// get machine instance for MAC
	mac := req.ClientHWAddr.String()
	log.WithField("mac", mac).Info("looking for machine instance")
	i := k.getKubevirtInstanceForMAC(mac)
	if i == nil {
		log.WithField("mac", mac).Info("no machine instance found")
		return nil, true
	}
	resp.UpdateOption(dhcpv4.OptHostName(i.Name))
	return resp, false
}

func (k *KubevirtState) getKubevirtInstanceForMAC(mac string) *KubevirtInstance {
	log.WithField("mac", mac).Info("looking for machine instance")
	log.WithField("instances", len(k.Instances)).Info("number of instances")
	// instances
	log.WithField("instances", k.Instances).Info("instances")
	for _, i := range k.Instances {
		log.WithField("checking instance", i).Info("instance")
		for _, j := range i.Interfaces {
			if j.MAC == mac {
				return &i
			}
		}
	}
	log.WithField("mac", mac).Info("no machine instance found")
	return nil
}

// addKubevirtInstance
func (k *KubevirtState) addKubevirtInstance(i *KubevirtInstance) {
	log.WithField("instance", i).Info("adding instance")
	// append if new, update if exists
	for idx, j := range k.Instances {
		if j.Name == i.Name && j.Namespace == i.Namespace {
			k.Instances[idx] = *i
			return
		}
	}
	k.Instances = append(k.Instances, *i)
	log.WithField("instances", len(k.Instances)).Info("number of instances")
}

// refreshKubevirtInstances
func (k *KubevirtState) refreshKubevirtInstances() error {
	vmi, err := k.Client.KubevirtV1().VirtualMachineInstances(v1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("failed to list virtual machine instances")
		return err
	}
	k.Instances = []KubevirtInstance{}
	for _, v := range vmi.Items {
		log.WithField("name", v.Name).Info("found virtual machine instance")
		k.addKubevirtInstance(&KubevirtInstance{
			Name:       v.Name,
			Namespace:  v.Namespace,
			Interfaces: v.Status.Interfaces,
		})
	}
	return nil
}
