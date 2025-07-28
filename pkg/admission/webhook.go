package admission

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/sirupsen/logrus"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ImageSizeGatekeeper/pkg/config"
	"github.com/ImageSizeGatekeeper/pkg/docker"
)

// 常量定义
const (
	// 指定原仓库的注解
	OriginalRegistryAnnotation = "imagesizegatekeeper.k8s.io/original-registry"
	// 指定认证Secret的注解
	CredentialsSecretAnnotation = "imagesizegatekeeper.k8s.io/credentials-secret"
)

// Webhook 定义了Admission Webhook的结构
type Webhook struct {
	config         *config.Config
	registryClient *docker.RegistryClient
}

// NewWebhook 创建一个新的Webhook实例
func NewWebhook(cfg *config.Config) (*Webhook, error) {
	registryClient := docker.NewRegistryClient(cfg)
	return &Webhook{
		config:         cfg,
		registryClient: registryClient,
	}, nil
}

// Handle 处理admission webhook请求
func (wh *Webhook) Handle(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if len(body) == 0 {
		logrus.Error("Empty body")
		http.Error(w, "Empty body", http.StatusBadRequest)
		return
	}

	// 解析请求
	admissionReview := admissionv1.AdmissionReview{}
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		logrus.Errorf("解析admission请求失败: %v", err)
		http.Error(w, fmt.Sprintf("解析admission请求失败: %v", err), http.StatusBadRequest)
		return
	}

	// 检查请求类型
	if admissionReview.Request == nil {
		logrus.Error("Invalid admission review request")
		http.Error(w, "Invalid admission review request", http.StatusBadRequest)
		return
	}

	// 处理请求
	var result *admissionv1.AdmissionResponse

	// 针对Pod资源进行处理
	if admissionReview.Request.Resource.Resource == "pods" {
		result = wh.validatePod(admissionReview.Request)
	} else {
		// 对其他资源类型放行
		result = &admissionv1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "资源不是Pod，放行",
			},
		}
	}

	// 构建响应
	admissionReview.Response = result
	admissionReview.Response.UID = admissionReview.Request.UID

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		logrus.Errorf("序列化响应失败: %v", err)
		http.Error(w, fmt.Sprintf("序列化响应失败: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// validatePod 验证Pod是否符合镜像大小限制
func (wh *Webhook) validatePod(request *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	// 检查命名空间是否受限制
	restriction := wh.config.GetNamespaceRestriction(request.Namespace)
	if restriction == nil {
		// 不受限制的命名空间，放行
		return &admissionv1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "命名空间不受限制，放行",
			},
		}
	}

	// 解析Pod对象
	var pod corev1.Pod
	if err := json.Unmarshal(request.Object.Raw, &pod); err != nil {
		logrus.Errorf("解析Pod对象失败: %v", err)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("解析Pod对象失败: %v", err),
			},
		}
	}

	// 获取原始仓库信息和认证Secret
	originalRegistry := ""
	credentialsSecret := "" // 存储认证Secret名称

	if pod.Annotations != nil {
		if reg, ok := pod.Annotations[OriginalRegistryAnnotation]; ok {
			originalRegistry = reg
		}
		if secret, ok := pod.Annotations[CredentialsSecretAnnotation]; ok {
			credentialsSecret = secret
		}
	}

	// 如果需要指定原始仓库但没有提供
	if restriction.RequireOrigReg && originalRegistry == "" {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("未指定原始仓库，请添加 %s 注解", OriginalRegistryAnnotation),
			},
		}
	}

	// 检查所有容器的镜像大小
	for _, container := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		// 获取镜像大小，传递认证Secret信息
		sizeInMB, err := wh.registryClient.GetImageSizeMB(container.Image, originalRegistry, request.Namespace, credentialsSecret)
		if err != nil {
			logrus.Errorf("获取镜像大小失败: %v, 镜像: %s", err, container.Image)
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("获取镜像 %s 大小失败: %v", container.Image, err),
				},
			}
		}

		// 检查大小是否超过限制
		if sizeInMB > restriction.MaxSizeMB {
			logrus.Warnf("镜像大小超过限制: %s, 大小: %.2fMB, 限制: %.2fMB",
				container.Image, sizeInMB, restriction.MaxSizeMB)
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("镜像 %s 大小 (%.2f MB) 超过限制 (%.2f MB)",
						container.Image, sizeInMB, restriction.MaxSizeMB),
				},
			}
		}

		logrus.Infof("镜像大小检查通过: %s, 大小: %.2fMB, 限制: %.2fMB",
			container.Image, sizeInMB, restriction.MaxSizeMB)
	}

	// 所有检查都通过
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Result: &metav1.Status{
			Message: "镜像大小检查通过",
		},
	}
}
