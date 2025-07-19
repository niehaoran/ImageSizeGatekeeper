package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// RegistryMapping 定义仓库映射，用于处理加速站的情况
type RegistryMapping struct {
	// 用户使用的仓库地址
	UserRegistry string `json:"userRegistry"`
	// 实际需要请求API的仓库地址
	ActualRegistry string `json:"actualRegistry"`
}

// RegistryCredential 定义仓库认证凭据
type RegistryCredential struct {
	// 仓库地址
	Registry string `json:"registry"`
	// 用户名
	Username string `json:"username"`
	// 密码
	Password string `json:"password"`
}

// NamespaceLimit 定义命名空间的大小限制
type NamespaceLimit struct {
	// 命名空间匹配模式，支持正则表达式
	Pattern string `json:"pattern"`
	// 已编译的正则表达式
	CompiledPattern *regexp.Regexp `json:"-"`
	// 最大镜像大小限制（GB）
	MaxSizeGB float64 `json:"maxSizeGB"`
}

// ProxyConfig 定义代理配置
type ProxyConfig struct {
	// 是否启用代理
	Enabled bool `json:"enabled"`
	// 代理URL
	URL string `json:"url"`
}

// Config 定义整个配置
type Config struct {
	// 互斥锁，保证配置读写安全
	mu sync.RWMutex
	// 仓库映射列表
	RegistryMappings []RegistryMapping `json:"registryMappings"`
	// 命名空间限制列表
	NamespaceLimits []NamespaceLimit `json:"namespaceLimits"`
	// 代理配置
	Proxy ProxyConfig `json:"proxy"`
	// 仓库认证信息列表（现在从Secret中加载，不从ConfigMap读取）
	RegistryCredentials []RegistryCredential `json:"-"`
	// 敏感信息目录
	SecretsDir string `json:"-"`
}

// 敏感文件常量
const (
	// 代理URL文件名
	ProxyURLFile = "proxy-url"
	// 仓库认证信息目录
	RegistryCredsDir = "registry-credentials"
)

// NewConfig 创建一个新的配置实例
func NewConfig() *Config {
	return &Config{
		RegistryMappings:    []RegistryMapping{},
		NamespaceLimits:     []NamespaceLimit{},
		RegistryCredentials: []RegistryCredential{},
		Proxy: ProxyConfig{
			Enabled: false,
		},
	}
}

// LoadSecrets 从指定目录加载敏感信息
func (c *Config) LoadSecrets(secretsDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.SecretsDir = secretsDir

	// 加载代理URL（如果存在）
	proxyURLPath := filepath.Join(secretsDir, ProxyURLFile)
	if _, err := os.Stat(proxyURLPath); err == nil {
		proxyURLData, err := ioutil.ReadFile(proxyURLPath)
		if err == nil && len(proxyURLData) > 0 {
			c.Proxy.URL = strings.TrimSpace(string(proxyURLData))
			c.Proxy.Enabled = true
		}
	}

	// 加载仓库认证信息
	credsDir := filepath.Join(secretsDir, RegistryCredsDir)
	if _, err := os.Stat(credsDir); err == nil {
		// 读取目录中的所有文件
		files, err := ioutil.ReadDir(credsDir)
		if err != nil {
			return fmt.Errorf("读取仓库认证目录失败: %v", err)
		}

		// 清空当前认证信息
		c.RegistryCredentials = []RegistryCredential{}

		// 遍历每个文件
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// 文件名即为仓库地址
			registry := file.Name()
			credPath := filepath.Join(credsDir, registry)

			// 读取认证信息
			credData, err := ioutil.ReadFile(credPath)
			if err != nil {
				return fmt.Errorf("读取仓库认证信息失败 %s: %v", registry, err)
			}

			// 解析用户名:密码格式
			credString := strings.TrimSpace(string(credData))
			credParts := strings.SplitN(credString, ":", 2)
			if len(credParts) == 2 {
				c.RegistryCredentials = append(c.RegistryCredentials, RegistryCredential{
					Registry: registry,
					Username: credParts[0],
					Password: credParts[1],
				})
			}
		}
	}

	return nil
}

// AddRegistryMapping 添加一个仓库映射
func (c *Config) AddRegistryMapping(userRegistry, actualRegistry string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.RegistryMappings = append(c.RegistryMappings, RegistryMapping{
		UserRegistry:   userRegistry,
		ActualRegistry: actualRegistry,
	})
}

// AddNamespaceLimit 添加一个命名空间限制
func (c *Config) AddNamespaceLimit(pattern string, maxSizeGB float64) error {
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.NamespaceLimits = append(c.NamespaceLimits, NamespaceLimit{
		Pattern:         pattern,
		CompiledPattern: compiledPattern,
		MaxSizeGB:       maxSizeGB,
	})
	return nil
}

// AddRegistryCredential 添加一个仓库认证信息
func (c *Config) AddRegistryCredential(registry, username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.RegistryCredentials = append(c.RegistryCredentials, RegistryCredential{
		Registry: registry,
		Username: username,
		Password: password,
	})
}

// SetProxy 设置代理配置
func (c *Config) SetProxy(enabled bool, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Proxy.Enabled = enabled
	c.Proxy.URL = url
}

// GetActualRegistry 根据用户仓库获取实际仓库
func (c *Config) GetActualRegistry(userRegistry string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, mapping := range c.RegistryMappings {
		if mapping.UserRegistry == userRegistry {
			return mapping.ActualRegistry
		}
	}

	// 如果没有找到映射，则返回原始仓库
	return userRegistry
}

// GetRegistryCredentials 获取仓库的认证信息
func (c *Config) GetRegistryCredentials(registry string) (string, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, cred := range c.RegistryCredentials {
		if cred.Registry == registry {
			return cred.Username, cred.Password, true
		}
	}

	// 如果没有找到认证信息，返回false
	return "", "", false
}

// GetNamespaceLimit 获取命名空间的大小限制
func (c *Config) GetNamespaceLimit(namespace string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, limit := range c.NamespaceLimits {
		if limit.CompiledPattern.MatchString(namespace) {
			return limit.MaxSizeGB, true
		}
	}

	// 如果没有找到匹配的限制，返回false
	return 0, false
}

// GetProxyConfig 获取代理配置
func (c *Config) GetProxyConfig() (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Proxy.Enabled, c.Proxy.URL
}
