package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/ImageSizeGatekeeper/pkg/admission"
	"github.com/ImageSizeGatekeeper/pkg/watcher"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 初始化配置监视器
	configWatcher, err := watcher.NewConfigWatcher(configPath)
	if err != nil {
		logrus.Fatalf("无法加载配置: %v", err)
	}
	// 启动配置监视
	configWatcher.Start()
	defer configWatcher.Stop()

	// 获取初始配置
	cfg := configWatcher.GetConfig()

	// 设置日志级别
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		logrus.Warnf("无效的日志级别 %s, 使用默认值 info", cfg.LogLevel)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.JSONFormatter{})

	// 创建webhook处理器
	webhook, err := admission.NewWebhook(cfg)
	if err != nil {
		logrus.Fatalf("创建webhook失败: %v", err)
	}

	// 设置HTTP路由
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/webhook", webhook.Handle)

	// 启动HTTP服务器
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	go func() {
		logrus.Infof("启动服务器，监听端口 %d", cfg.Port)
		if err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			if err != http.ErrServerClosed {
				logrus.Fatalf("服务器错误: %v", err)
			}
		}
	}()

	// 处理终止信号
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	logrus.Info("接收到终止信号，关闭服务器...")
}
