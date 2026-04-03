package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/andrew/kymaadapterstub/control-plane/models"
)

var apiRuleGVR = schema.GroupVersionResource{
	Group:    "gateway.kyma-project.io",
	Version:  "v2",
	Resource: "apirules",
}

type Client struct {
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	config        *rest.Config
	clusterDomain string
}

func NewClient(clusterDomain string) (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Client{
		clientset:     clientset,
		dynamicClient: dynamicClient,
		config:        config,
		clusterDomain: clusterDomain,
	}, nil
}

// CreateAdapterDeployment creates a Kubernetes Deployment for an adapter
func (c *Client) CreateAdapterDeployment(namespace string, adapter models.Adapter, controlPlaneURL string) error {
	// Determine image based on adapter type
	image := getAdapterImage(adapter.Type)

	labels := map[string]string{
		"app":          "adapter",
		"adapter-id":   sanitizeLabel(adapter.ID),
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
					Annotations: func() map[string]string {
						if adapter.Type == "SFTP" {
							return map[string]string{
								"traffic.sidecar.istio.io/excludeInboundPorts": "22",
							}
						}
						return nil
					}(),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "ghcr-secret"},
					},
					Containers: []corev1.Container{
						{
							Name:  strings.ToLower(adapter.Type) + "-adapter",
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
		"adapter-id":   sanitizeLabel(adapter.ID),
		"adapter-type": adapter.Type,
	}

	var servicePorts []corev1.ServicePort
	var serviceType corev1.ServiceType

	if adapter.Type == "SFTP" {
		// SFTP needs a LoadBalancer for external SSH access
		serviceType = corev1.ServiceTypeLoadBalancer
		servicePorts = []corev1.ServicePort{
			{
				Name:       "sftp",
				Port:       22,
				TargetPort: intOrString(22),
				Protocol:   corev1.ProtocolTCP,
			},
		}
	} else {
		// REST/OData use ClusterIP — external access via APIRule
		serviceType = corev1.ServiceTypeClusterIP
		servicePorts = []corev1.ServicePort{
			{
				Name:       "http",
				Port:       80,
				TargetPort: intOrString(8080),
				Protocol:   corev1.ProtocolTCP,
			},
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adapter.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labels,
			Ports:    servicePorts,
		},
	}

	_, err := c.clientset.CoreV1().Services(namespace).Create(context.Background(), service, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create service: %w", err)
	}

	log.Printf("Created %s service %s in namespace %s", serviceType, adapter.Name, namespace)
	return "", nil
}

// GetLoadBalancerHostname waits for a LoadBalancer service to get an external hostname
func (c *Client) GetLoadBalancerHostname(namespace, serviceName string) (string, error) {
	for i := 0; i < 24; i++ { // wait up to 2 minutes
		svc, err := c.clientset.CoreV1().Services(namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get service: %w", err)
		}

		ingress := svc.Status.LoadBalancer.Ingress
		if len(ingress) > 0 {
			if ingress[0].Hostname != "" {
				return ingress[0].Hostname, nil
			}
			if ingress[0].IP != "" {
				return ingress[0].IP, nil
			}
		}

		log.Printf("Waiting for LoadBalancer hostname for service %s (%d/24)...", serviceName, i+1)
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for LoadBalancer hostname")
}

// CleanupOrphanedResources deletes all adapter deployments, services and APIRules
// regardless of whether the control plane knows about them
func (c *Client) CleanupOrphanedResources(namespace string) error {
	labelSelector := "app=adapter"

	// Delete deployments
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Printf("Warning: failed to list adapter deployments: %v", err)
	} else {
		for _, d := range deployments.Items {
			if err := c.clientset.AppsV1().Deployments(namespace).Delete(context.Background(), d.Name, metav1.DeleteOptions{}); err != nil {
				log.Printf("Warning: failed to delete deployment %s: %v", d.Name, err)
			} else {
				log.Printf("Deleted orphaned deployment: %s", d.Name)
			}
		}
	}

	// Delete services
	services, err := c.clientset.CoreV1().Services(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Printf("Warning: failed to list adapter services: %v", err)
	} else {
		for _, s := range services.Items {
			if err := c.clientset.CoreV1().Services(namespace).Delete(context.Background(), s.Name, metav1.DeleteOptions{}); err != nil {
				log.Printf("Warning: failed to delete service %s: %v", s.Name, err)
			} else {
				log.Printf("Deleted orphaned service: %s", s.Name)
			}
		}
	}

	// Delete APIRules
	apiRules, err := c.dynamicClient.Resource(apiRuleGVR).Namespace(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Printf("Warning: failed to list adapter APIRules: %v", err)
	} else {
		for _, r := range apiRules.Items {
			if err := c.dynamicClient.Resource(apiRuleGVR).Namespace(namespace).Delete(context.Background(), r.GetName(), metav1.DeleteOptions{}); err != nil {
				log.Printf("Warning: failed to delete APIRule %s: %v", r.GetName(), err)
			} else {
				log.Printf("Deleted orphaned APIRule: %s", r.GetName())
			}
		}
	}

	return nil
}

// StopAdapterDeployment scales a deployment to 0 replicas (stops without deleting)
func (c *Client) StopAdapterDeployment(namespace string, adapter models.Adapter) error {
	scale, err := c.clientset.AppsV1().Deployments(namespace).GetScale(context.Background(), adapter.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment scale: %w", err)
	}

	scale.Spec.Replicas = 0
	_, err = c.clientset.AppsV1().Deployments(namespace).UpdateScale(context.Background(), adapter.DeploymentName, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment to 0: %w", err)
	}

	log.Printf("Stopped deployment %s in namespace %s", adapter.DeploymentName, namespace)
	return nil
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

// CreateAdapterAPIRule creates a Kyma APIRule to expose an adapter publicly
// Only applies to HTTP-based adapters (REST, OData) — not SFTP
func (c *Client) CreateAdapterAPIRule(namespace string, adapter models.Adapter) (string, error) {
	if adapter.Type == "SFTP" {
		return "", nil
	}

	if c.clusterDomain == "" {
		return "", fmt.Errorf("cluster domain not configured")
	}

	host := fmt.Sprintf("%s.%s", adapter.Name, c.clusterDomain)

	apiRule := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.kyma-project.io/v2",
			"kind":       "APIRule",
			"metadata": map[string]interface{}{
				"name":      adapter.Name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"app":          "adapter",
					"adapter-id":   sanitizeLabel(adapter.ID),
					"adapter-type": adapter.Type,
				},
			},
			"spec": map[string]interface{}{
				"gateway": "kyma-system/kyma-gateway",
				"hosts":   []interface{}{host},
				"service": map[string]interface{}{
					"name": adapter.Name,
					"port": int64(80),
				},
				"rules": []interface{}{
					map[string]interface{}{
						"path":    "/{**}",
						"methods": []interface{}{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
						"noAuth":  true,
					},
				},
			},
		},
	}

	_, err := c.dynamicClient.Resource(apiRuleGVR).Namespace(namespace).Create(context.Background(), apiRule, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create APIRule: %w", err)
	}

	publicURL := fmt.Sprintf("https://%s", host)
	log.Printf("Created APIRule for adapter %s: %s", adapter.Name, publicURL)
	return publicURL, nil
}

// DeleteAdapterAPIRule deletes the APIRule for an adapter
func (c *Client) DeleteAdapterAPIRule(namespace string, adapter models.Adapter) error {
	if adapter.Type == "SFTP" {
		return nil
	}

	err := c.dynamicClient.Resource(apiRuleGVR).Namespace(namespace).Delete(context.Background(), adapter.Name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete APIRule: %w", err)
	}

	log.Printf("Deleted APIRule for adapter %s", adapter.Name)
	return nil
}

// GetAdapterLogs returns the last `tail` lines of logs from the adapter's pod.
func (c *Client) GetAdapterLogs(namespace, adapterID string, tail int64) (string, error) {
	pods, err := c.clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("adapter-id=%s", sanitizeLabel(adapterID)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return "(no pods found — adapter may not be running)", nil
	}

	pod := pods.Items[0]
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := c.clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, opts)
	stream, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to stream logs: %w", err)
	}
	defer stream.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, stream); err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}
	return buf.String(), nil
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

// sanitizeLabel replaces characters invalid in Kubernetes label values with hyphens
func sanitizeLabel(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			result[i] = c
		} else {
			result[i] = '-'
		}
	}
	// Trim leading/trailing hyphens
	return strings.Trim(string(result), "-")
}

func getAdapterImage(adapterType string) string {
	switch adapterType {
	case "REST":
		return "ghcr.io/achgithub/kymaadapterstub-rest-adapter:latest"
	case "SFTP":
		return "ghcr.io/achgithub/kymaadapterstub-sftp-adapter:latest"
	case "OData":
		return "ghcr.io/achgithub/kymaadapterstub-odata-adapter:latest"
	case "SOAP":
		return "ghcr.io/achgithub/kymaadapterstub-soap-adapter:latest"
	case "XI":
		return "ghcr.io/achgithub/kymaadapterstub-xi-adapter:latest"
	case "AS2":
		return "ghcr.io/achgithub/kymaadapterstub-as2-adapter:latest"
	case "AS4":
		return "ghcr.io/achgithub/kymaadapterstub-as4-adapter:latest"
	case "EDIFACT":
		return "ghcr.io/achgithub/kymaadapterstub-edifact-adapter:latest"
	case "REST-SENDER", "SOAP-SENDER", "XI-SENDER":
		return "ghcr.io/achgithub/kymaadapterstub-sender-adapter:latest"
	default:
		return "ghcr.io/achgithub/kymaadapterstub-rest-adapter:latest"
	}
}
