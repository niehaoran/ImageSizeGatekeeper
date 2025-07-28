package config

import (
	"io/ioutil"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 包含应用程序的所有配置
type Config struct {
	// 服务器配置
	Port        int    `yaml:"port"`
	TLSCertFile string `yaml:"tlsCertFile"`
	TLSKeyFile  string `yaml:"tlsKeyFile"`
	LogLevel    string `yaml:"logLevel"`

	// 代理配置
	ProxyEnabled bool   `yaml:"proxyEnabled"`
	ProxyURL     string `yaml:"proxyURL"`

	// 认证配置
	RegistryAuth map[string]RegistryAuth `yaml:"registryAuth"`

	// 命名空间限制配置
	NamespaceRestrictions map[string]NamespaceRestriction `yaml:"namespaceRestrictions"`

	// 缓存已编译的正则表达式
	compiledRegexps map[string]*regexp.Regexp
}

// RegistryAuth 定义了镜像仓库的认证信息
type RegistryAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// NamespaceRestriction 定义了命名空间的限制规则
type NamespaceRestriction struct {
	Enabled        bool    `yaml:"enabled"`
	MaxSizeMB      float64 `yaml:"maxSizeMB"`
	RequireOrigReg bool    `yaml:"requireOriginalRegistry"`
	IsRegex        bool    `yaml:"isRegex"` // 标识是否为正则表达式
}

// LoadConfig 从指定路径加载配置
func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// 设置默认值
	if config.LogLevel == "" {
		config.LogLevel = "info"
	}
	if config.Port == 0 {
		config.Port = 8443
	}

	// 预编译正则表达式
	config.compiledRegexps = make(map[string]*regexp.Regexp)
	for nsPattern, restriction := range config.NamespaceRestrictions {
		if restriction.IsRegex {
			re, err := regexp.Compile(nsPattern)
			if err != nil {
				continue // 忽略无效的正则表达式
			}
			config.compiledRegexps[nsPattern] = re
		}
	}

	return &config, nil
}

// GetRegistryAuth 根据镜像仓库获取认证信息
func (c *Config) GetRegistryAuth(registry string) *RegistryAuth {
	// 移除可能的协议前缀
	registry = strings.TrimPrefix(registry, "http://")
	registry = strings.TrimPrefix(registry, "https://")

	// 尝试直接匹配
	if auth, ok := c.RegistryAuth[registry]; ok {
		return &auth
	}

	// 尝试匹配子域名
	for reg, auth := range c.RegistryAuth {
		if strings.HasSuffix(registry, "."+reg) || strings.HasPrefix(registry, reg+".") {
			return &auth
		}
	}

	return nil
}

// IsNamespaceRestricted 检查命名空间是否受限制
func (c *Config) IsNamespaceRestricted(namespace string) bool {
	// 精确匹配
	if restriction, ok := c.NamespaceRestrictions[namespace]; ok {
		return restriction.Enabled
	}

	// 正则表达式匹配
	for pattern, re := range c.compiledRegexps {
		restriction := c.NamespaceRestrictions[pattern]
		if restriction.Enabled && re.MatchString(namespace) {
			return true
		}
	}

	return false
}

// GetNamespaceRestriction 获取命名空间的限制配置
func (c *Config) GetNamespaceRestriction(namespace string) *NamespaceRestriction {
	// 精确匹配
	if restriction, ok := c.NamespaceRestrictions[namespace]; ok && restriction.Enabled {
		return &restriction
	}

	// 正则表达式匹配
	for pattern, re := range c.compiledRegexps {
		restriction := c.NamespaceRestrictions[pattern]
		if restriction.Enabled && re.MatchString(namespace) {
			return &restriction
		}
	}

	return nil
}
