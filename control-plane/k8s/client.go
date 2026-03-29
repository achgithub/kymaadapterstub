package k8s

import (
	"context"
	"fmt"
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/andrew/kymaadapterstub/control-plane/models"
)

type Client struct {
	clientset kubernetes.Interface
	config    *rest.Config
}

func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &Client{
		clientset: clientset,
		config:    config,
	}, nil
}

// CreateAdapterDeployment creates a Kubernetes Deployment for an adapter
func (c *Client) CreateAdapterDeployment(namespace string, adapter models.Adapter, controlPlaneURL string) error {
	// Determine image based on adapter type
	image := getAdapterImage(adapter.Type)

	labels := map[string]string{
		"app":          "adapter",
		"adapter-id":   adapter.ID,
		"adapter-type": adapter.Type,
	}

	// Environment variables for adapter
	env := []corev1.EnvVar{
		{
			Name:  "ADAPTER_ID",
			Value: adapter.ID,
		},
		{
			Name:  "CONTROL_PLANE_URL",
			Value: controlPlaneURL,
		},
	}

	// SFTP adapters need SSH port
	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: 8080,
			Protocol:      corev1.ProtocolTCP,
		},
	}

	if adapter.Type == "SFTP" {
		ports = append(ports, corev1.ContainerPort{
			Name:          "sftp",
			ContainerPort: 22,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adapter.DeploymentName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  adapter.Type + "-adapter",
							Image: image,
							Ports: ports,
							Env:   env,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: parseQuantity("64Mi"),
									corev1.ResourceCPU:    parseQuantity("50m"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: parseQuantity("256Mi"),
									corev1.ResourceCPU:    parseQuantity("200m"),
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := c.clientset.AppsV1().Deployments(namespace).Create(context.Background(), deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	log.Printf("Created deployment %s in namespace %s", adapter.DeploymentName, namespace)
	return nil
}

// CreateAdapterService creates a Kubernetes Service for an adapter
func (c *Client) CreateAdapterService(namespace string, adapter models.Adapter) (string, error) {
	labels := map[string]string{
		"app":          "adapter",
		"adapter-id":   adapter.ID,
		"adapter-type": adapter.Type,
	}

	servicePorts := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       80,
			TargetPort: intOrString(8080),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	if adapter.Type == "SFTP" {
		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       "sftp",
			Port:       22,
			TargetPort: intOrString(22),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adapter.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    servicePorts,
		},
	}

	_, err := c.clientset.CoreV1().Services(namespace).Create(context.Background(), service, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create service: %w", err)
	}

	log.Printf("Created service %s in namespace %s", adapter.Name, namespace)

	// Service DNS name in Kubernetes
	serviceDNS := fmt.Sprintf("%s.%s.svc.cluster.local", adapter.Name, namespace)
	return serviceDNS, nil
}

// DeleteAdapterResources deletes Deployment and Service for an adapter
func (c *Client) DeleteAdapterResources(namespace string, adapter models.Adapter) error {
	// Delete deployment
	err := c.clientset.AppsV1().Deployments(namespace).Delete(context.Background(), adapter.DeploymentName, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("Warning: failed to delete deployment %s: %v", adapter.DeploymentName, err)
	}

	// Delete service
	err = c.clientset.CoreV1().Services(namespace).Delete(context.Background(), adapter.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("Warning: failed to delete service %s: %v", adapter.Name, err)
	}

	log.Printf("Deleted resources for adapter %s", adapter.ID)
	return nil
}

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}

func intOrString(i int32) intstr.IntOrString {
	return intstr.FromInt32(i)
}

func parseQuantity(s string) resource.Quantity {
	q, _ := resource.ParseQuantity(s)
	return q
}

func getAdapterImage(adapterType string) string {
	switch adapterType {
	case "REST":
		return "ghcr.io/achgithub/kymaadapterstub-rest-adapter:latest"
	case "SFTP":
		return "ghcr.io/achgithub/kymaadapterstub-sftp-adapter:latest"
	case "OData":
		return "ghcr.io/achgithub/kymaadapterstub-odata-adapter:latest"
	default:
		return "ghcr.io/achgithub/kymaadapterstub-rest-adapter:latest"
	}
}
