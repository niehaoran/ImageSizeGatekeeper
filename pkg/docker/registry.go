package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/ImageSizeGatekeeper/pkg/config"
)

// ImageInfo 包含镜像的大小信息
type ImageInfo struct {
	CompressedSize   float64
	UncompressedSize float64
	HasAccurateSize  bool
}

// RegistryAuth 存储从Secret中获取的认证信息
type RegistryAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RegistryAuthMap 存储多个仓库的认证信息
type RegistryAuthMap struct {
	Registries map[string]RegistryAuth `json:"registries"`
}

// RegistryClient 用于与Docker镜像仓库交互
type RegistryClient struct {
	Config     *config.Config
	KubeClient kubernetes.Interface // K8s客户端用于获取Secret
}

// NewRegistryClient 创建一个新的Docker镜像仓库客户端
func NewRegistryClient(cfg *config.Config) *RegistryClient {
	// 创建K8s客户端
	k8sClient, err := createK8sClient()
	if err != nil {
		logrus.Warnf("无法创建K8s客户端，将不能从Secret获取认证信息: %v", err)
	}

	return &RegistryClient{
		Config:     cfg,
		KubeClient: k8sClient,
	}
}

// createK8sClient 创建Kubernetes客户端
func createK8sClient() (kubernetes.Interface, error) {
	// 使用集群内配置
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

// GetImageSize 获取Docker镜像的大小信息
// 基于image_size.sh脚本的逻辑
func (c *RegistryClient) GetImageSize(image string, originalRegistry string, namespace string, credentialsSecret string) (*ImageInfo, error) {
	// 检查镜像名是否带标签，未带则补全为:latest
	if !strings.Contains(image, ":") {
		image = image + ":latest"
	}

	// 构建镜像路径
	targetImage := image
	if originalRegistry != "" {
		// 如果提供了原始仓库，使用它替换
		parts := strings.SplitN(image, "/", 2)
		if len(parts) == 2 {
			targetImage = originalRegistry + "/" + parts[1]
		}
	}

	// 尝试获取镜像信息
	return c.fetchImageInfo(targetImage, namespace, credentialsSecret)
}

// fetchImageInfo 使用skopeo获取镜像信息
func (c *RegistryClient) fetchImageInfo(image string, namespace string, credentialsSecret string) (*ImageInfo, error) {
	// 构建skopeo命令
	args := []string{"inspect"}

	// 添加认证信息
	registry := strings.Split(image, "/")[0]

	// 首先尝试从Secret获取认证信息（如果提供了Secret）
	if credentialsSecret != "" && c.KubeClient != nil {
		auth, err := c.getAuthFromSecret(registry, namespace, credentialsSecret)
		if err == nil && auth != nil {
			logrus.Infof("使用Secret '%s' 中的认证信息访问仓库 '%s'", credentialsSecret, registry)
			args = append(args, "--creds", fmt.Sprintf("%s:%s", auth.Username, auth.Password))
		} else {
			logrus.Warnf("无法从Secret获取认证信息: %v", err)
		}
	} else {
		// 使用配置文件中的认证信息作为后备
		auth := c.Config.GetRegistryAuth(registry)
		if auth != nil {
			args = append(args, "--creds", fmt.Sprintf("%s:%s", auth.Username, auth.Password))
		}
	}

	// 添加镜像路径
	args = append(args, "docker://"+image)

	// 创建命令
	cmd := exec.Command("skopeo", args...)

	// 配置环境变量（代理）
	env := []string{}
	if c.Config.ProxyEnabled && c.Config.ProxyURL != "" {
		env = append(env, "HTTPS_PROXY="+c.Config.ProxyURL, "HTTP_PROXY="+c.Config.ProxyURL)
	}
	cmd.Env = append(cmd.Environ(), env...)

	// 设置超时
	timer := time.AfterFunc(30*time.Second, func() {
		cmd.Process.Kill()
	})
	defer timer.Stop()

	// 执行命令
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Errorf("获取镜像信息失败: %v, 输出: %s", err, string(output))
		if strings.Contains(string(output), "unauthorized") ||
			strings.Contains(string(output), "forbidden") ||
			strings.Contains(string(output), "not found") {
			return nil, fmt.Errorf("权限不足或镜像不存在: %v", err)
		}
		return nil, fmt.Errorf("获取镜像信息失败: %v", err)
	}

	// 解析JSON响应
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("解析镜像信息失败: %v", err)
	}

	// 提取层信息
	layers, ok := result["LayersData"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("未找到镜像层信息")
	}

	// 计算压缩和解压后的大小
	var compressedSize float64
	var uncompressedSize float64
	var hasAccurateSize bool = true

	for _, layer := range layers {
		layerMap, ok := layer.(map[string]interface{})
		if !ok {
			continue
		}

		// 压缩大小
		if size, ok := layerMap["Size"].(float64); ok {
			compressedSize += size
		}

		// 解压大小
		if size, ok := layerMap["UncompressedSize"].(float64); ok {
			uncompressedSize += size
		} else {
			hasAccurateSize = false
		}
	}

	// 如果没有解压大小，使用估算值
	if !hasAccurateSize || uncompressedSize == 0 {
		uncompressedSize = compressedSize * 1.7 // 估算系数
	}

	return &ImageInfo{
		CompressedSize:   compressedSize,
		UncompressedSize: uncompressedSize,
		HasAccurateSize:  hasAccurateSize,
	}, nil
}

// GetImageSizeMB 获取镜像大小（MB为单位）
func (c *RegistryClient) GetImageSizeMB(image string, originalRegistry string, namespace string, credentialsSecret string) (float64, error) {
	info, err := c.GetImageSize(image, originalRegistry, namespace, credentialsSecret)
	if err != nil {
		return 0, err
	}
	return info.UncompressedSize / 1024 / 1024, nil
}

// getAuthFromSecret 从Kubernetes Secret中获取认证信息
func (c *RegistryClient) getAuthFromSecret(registry string, namespace string, secretName string) (*RegistryAuth, error) {
	// 检查K8s客户端是否可用
	if c.KubeClient == nil {
		return nil, fmt.Errorf("无法访问Kubernetes API")
	}

	// 获取Secret
	secret, err := c.KubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取Secret失败: %v", err)
	}

	// 尝试解析auth.json格式
	if data, ok := secret.Data["auth.json"]; ok {
		var authMap RegistryAuthMap
		if err := json.Unmarshal(data, &authMap); err == nil {
			// 检查是否有当前仓库的认证信息
			if auth, exists := authMap.Registries[registry]; exists {
				return &auth, nil
			}
		}
	}

	// 尝试解析特定仓库的认证信息
	if data, ok := secret.Data[registry]; ok {
		var auth RegistryAuth
		if err := json.Unmarshal(data, &auth); err == nil {
			return &auth, nil
		}
	}

	// 没有找到认证信息
	return nil, fmt.Errorf("未找到仓库 %s 的认证信息", registry)
}
