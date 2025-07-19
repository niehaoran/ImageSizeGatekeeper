package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/yourusername/imagesizegatekeeper/pkg/config"
)

// 常量定义
const (
	// 默认超时时间
	defaultTimeout = 30 * time.Second
	// Docker Registry API v2 基础路径
	registryAPIv2 = "v2"
)

// ImageInfo 定义镜像信息
type ImageInfo struct {
	// 仓库名称
	Repository string
	// 镜像标签
	Tag string
	// 镜像大小（字节）
	SizeBytes int64
	// 镜像大小（GB，精确到小数点后两位）
	SizeGB float64
}

// RegistryClient 定义Docker Registry客户端
type RegistryClient struct {
	// HTTP客户端
	client *http.Client
	// 配置
	cfg *config.Config
}

// RegistryCredentials 定义仓库认证凭据
type RegistryCredentials struct {
	Username string
	Password string
}

// NewRegistryClient 创建一个新的Registry客户端
func NewRegistryClient(cfg *config.Config) *RegistryClient {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	// 如果启用了代理，设置代理
	proxyEnabled, proxyURL := cfg.GetProxyConfig()
	if proxyEnabled && proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxy),
			}
		}
	}

	return &RegistryClient{
		client: client,
		cfg:    cfg,
	}
}

// parseImageName 解析镜像名称，返回仓库地址、仓库名和标签
func parseImageName(imageName string) (string, string, string) {
	// 默认使用docker.io作为仓库地址
	registry := "docker.io"
	repository := imageName
	tag := "latest"

	// 解析标签
	parts := strings.Split(imageName, ":")
	if len(parts) > 1 {
		repository = parts[0]
		tag = parts[1]
	}

	// 解析仓库地址
	repoParts := strings.Split(repository, "/")
	if len(repoParts) > 1 && strings.Contains(repoParts[0], ".") {
		registry = repoParts[0]
		repository = strings.Join(repoParts[1:], "/")
	}

	return registry, repository, tag
}

// 解析WWW-Authenticate头
func parseAuthHeader(authHeader string) (string, string, error) {
	// 匹配Bearer realm="xxx",service="xxx"
	re := regexp.MustCompile(`Bearer realm="([^"]+)",service="([^"]+)"`)
	matches := re.FindStringSubmatch(authHeader)
	if len(matches) < 3 {
		return "", "", fmt.Errorf("无效的认证头: %s", authHeader)
	}
	return matches[1], matches[2], nil
}

// 获取认证Token
func (rc *RegistryClient) getAuthToken(realm, service, repository string, creds *RegistryCredentials) (string, error) {
	// 构建token请求URL
	tokenURL, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("解析realm失败: %v", err)
	}

	q := tokenURL.Query()
	q.Add("service", service)
	q.Add("scope", fmt.Sprintf("repository:%s:pull", repository))
	tokenURL.RawQuery = q.Encode()

	// 创建请求
	req, err := http.NewRequest("GET", tokenURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("创建token请求失败: %v", err)
	}

	// 添加认证信息（如果有）
	if creds != nil && creds.Username != "" && creds.Password != "" {
		req.SetBasicAuth(creds.Username, creds.Password)
	}

	// 发送请求
	resp, err := rc.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送token请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("获取token失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("解析token响应失败: %v", err)
	}

	// 有些Registry返回token，有些返回access_token
	token := tokenResp.Token
	if token == "" {
		token = tokenResp.AccessToken
	}

	if token == "" {
		return "", fmt.Errorf("响应中没有token")
	}

	return token, nil
}

// GetImageSize 获取镜像大小
func (rc *RegistryClient) GetImageSize(imageName string) (*ImageInfo, error) {
	// 解析镜像名称
	registry, repository, tag := parseImageName(imageName)

	// 获取实际仓库地址（考虑加速站的情况）
	actualRegistry := rc.cfg.GetActualRegistry(registry)

	// 从配置获取认证信息
	creds := rc.getCredentials(actualRegistry)

	// 构建API请求URL
	manifestURL := fmt.Sprintf("https://%s/%s/%s/manifests/%s",
		actualRegistry, registryAPIv2, repository, tag)

	// 创建请求
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 添加Accept头，请求Docker Registry API v2 schema 2
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	// 发送请求
	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理认证情况
	if resp.StatusCode == http.StatusUnauthorized {
		authHeader := resp.Header.Get("Www-Authenticate")
		if authHeader == "" {
			return nil, fmt.Errorf("需要认证，但没有提供认证方式")
		}

		realm, service, err := parseAuthHeader(authHeader)
		if err != nil {
			return nil, fmt.Errorf("解析认证头失败: %v", err)
		}

		// 获取token
		token, err := rc.getAuthToken(realm, service, repository, creds)
		if err != nil {
			return nil, fmt.Errorf("获取token失败: %v", err)
		}

		// 使用token重新发送请求
		req, err = http.NewRequest("GET", manifestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("创建带认证的请求失败: %v", err)
		}
		req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

		resp, err = rc.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("发送带认证的请求失败: %v", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var manifest struct {
		Config struct {
			Size int64 `json:"size"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	// 计算总大小：配置大小 + 所有层大小
	var totalSize int64 = manifest.Config.Size
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}

	// 转换为GB
	sizeGB := float64(totalSize) / (1024 * 1024 * 1024)

	return &ImageInfo{
		Repository: repository,
		Tag:        tag,
		SizeBytes:  totalSize,
		SizeGB:     sizeGB,
	}, nil
}

// getCredentials 从配置获取仓库的认证信息
func (rc *RegistryClient) getCredentials(registry string) *RegistryCredentials {
	// 从配置中获取认证信息
	username, password, found := rc.cfg.GetRegistryCredentials(registry)
	if !found {
		return nil
	}
	return &RegistryCredentials{
		Username: username,
		Password: password,
	}
}
