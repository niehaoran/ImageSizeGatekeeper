package admission

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/yourusername/imagesizegatekeeper/pkg/config"
	"github.com/yourusername/imagesizegatekeeper/pkg/docker"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

// ImageSizeWebhook 定义镜像大小验证的Webhook结构
type ImageSizeWebhook struct {
	// 配置
	config *config.Config
	// Docker Registry客户端
	registryClient *docker.RegistryClient
}

// NewImageSizeWebhook 创建一个新的ImageSizeWebhook实例
func NewImageSizeWebhook(cfg *config.Config) *ImageSizeWebhook {
	registryClient := docker.NewRegistryClient(cfg)
	return &ImageSizeWebhook{
		config:         cfg,
		registryClient: registryClient,
	}
}

// 处理/validate路径的HTTP请求
func (w *ImageSizeWebhook) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/validate" {
		http.Error(writer, "无效的路径", http.StatusNotFound)
		return
	}

	// 只处理POST请求
	if request.Method != http.MethodPost {
		http.Error(writer, "无效的方法", http.StatusMethodNotAllowed)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, fmt.Sprintf("读取请求体失败: %v", err), http.StatusBadRequest)
		return
	}

	// 设置响应类型
	contentType := request.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(writer, "不支持的内容类型", http.StatusUnsupportedMediaType)
		return
	}

	// 解析AdmissionReview请求
	var admissionReview admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		http.Error(writer, fmt.Sprintf("解析请求失败: %v", err), http.StatusBadRequest)
		return
	}

	// 如果请求为空，返回错误
	if admissionReview.Request == nil {
		http.Error(writer, "空的admission请求", http.StatusBadRequest)
		return
	}

	// 创建响应
	admissionResponse := w.validate(admissionReview.Request)

	// 创建完整的AdmissionReview响应
	response := admissionv1.AdmissionReview{
		TypeMeta: admissionReview.TypeMeta,
		Response: admissionResponse,
	}

	// 序列化并返回响应
	resp, err := json.Marshal(response)
	if err != nil {
		http.Error(writer, fmt.Sprintf("序列化响应失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 设置响应头
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)

	// 写入响应体
	if _, err := writer.Write(resp); err != nil {
		fmt.Printf("写入响应失败: %v", err)
	}
}

// validate 验证Pod是否满足镜像大小限制
func (w *ImageSizeWebhook) validate(request *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	// 只处理Pod资源
	if request.Kind.Kind != "Pod" {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: "非Pod资源，跳过验证",
			},
		}
	}

	// 解析Pod对象
	var pod corev1.Pod
	if err := json.Unmarshal(request.Object.Raw, &pod); err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("解析Pod对象失败: %v", err),
			},
		}
	}

	// 获取该命名空间的大小限制
	maxSizeGB, hasLimit := w.config.GetNamespaceLimit(request.Namespace)
	if !hasLimit {
		// 如果没有限制，允许创建
		return &admissionv1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: fmt.Sprintf("命名空间 %s 没有设置大小限制", request.Namespace),
			},
		}
	}

	// 检查每个容器的镜像大小
	for _, container := range pod.Spec.Containers {
		// 获取镜像大小
		imageInfo, err := w.registryClient.GetImageSize(container.Image)
		if err != nil {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("获取镜像 %s 大小失败: %v", container.Image, err),
				},
			}
		}

		// 检查大小是否超过限制
		if imageInfo.SizeGB > maxSizeGB {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf(
						"拒绝创建Pod：镜像 %s 大小 %.2f GB 超过命名空间 %s 的限制 %.2f GB",
						container.Image, imageInfo.SizeGB, request.Namespace, maxSizeGB,
					),
				},
			}
		}
	}

	// 检查初始化容器
	for _, container := range pod.Spec.InitContainers {
		// 获取镜像大小
		imageInfo, err := w.registryClient.GetImageSize(container.Image)
		if err != nil {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("获取初始化容器镜像 %s 大小失败: %v", container.Image, err),
				},
			}
		}

		// 检查大小是否超过限制
		if imageInfo.SizeGB > maxSizeGB {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf(
						"拒绝创建Pod：初始化容器镜像 %s 大小 %.2f GB 超过命名空间 %s 的限制 %.2f GB",
						container.Image, imageInfo.SizeGB, request.Namespace, maxSizeGB,
					),
				},
			}
		}
	}

	// 所有检查都通过，允许创建
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Result: &metav1.Status{
			Message: "所有镜像大小都在限制范围内",
		},
	}
}
