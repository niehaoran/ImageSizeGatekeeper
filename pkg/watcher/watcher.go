package watcher

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ImageSizeGatekeeper/pkg/config"
	"github.com/sirupsen/logrus"
)

// ConfigWatcher 用于监视配置文件变更
type ConfigWatcher struct {
	configPath string
	config     *config.Config
	lastMod    time.Time
	interval   time.Duration
	mutex      sync.RWMutex
	stopCh     chan struct{}
}

// NewConfigWatcher 创建一个新的配置监视器
func NewConfigWatcher(configPath string) (*ConfigWatcher, error) {
	// 获取初始配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	// 获取文件修改时间
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("无法获取配置文件信息: %v", err)
	}

	return &ConfigWatcher{
		configPath: configPath,
		config:     cfg,
		lastMod:    info.ModTime(),
		interval:   30 * time.Second, // 默认每30秒检查一次
		stopCh:     make(chan struct{}),
	}, nil
}

// Start 开始监视配置文件变更
func (w *ConfigWatcher) Start() {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.checkAndReload()
			case <-w.stopCh:
				return
			}
		}
	}()

	logrus.Info("配置监视器已启动")
}

// Stop 停止监视配置文件变更
func (w *ConfigWatcher) Stop() {
	close(w.stopCh)
	logrus.Info("配置监视器已停止")
}

// GetConfig 获取当前配置
func (w *ConfigWatcher) GetConfig() *config.Config {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.config
}

// checkAndReload 检查配置文件是否变更并重新加载
func (w *ConfigWatcher) checkAndReload() {
	info, err := os.Stat(w.configPath)
	if err != nil {
		logrus.Errorf("检查配置文件失败: %v", err)
		return
	}

	// 如果文件修改时间变更，则重新加载配置
	if info.ModTime().After(w.lastMod) {
		logrus.Info("检测到配置文件变更，重新加载")

		cfg, err := config.LoadConfig(w.configPath)
		if err != nil {
			logrus.Errorf("重新加载配置失败: %v", err)
			return
		}

		// 更新配置
		w.mutex.Lock()
		w.config = cfg
		w.lastMod = info.ModTime()
		w.mutex.Unlock()

		logrus.Info("配置重新加载成功")
	}
}
