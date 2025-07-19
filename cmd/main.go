package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/yourusername/imagesizegatekeeper/pkg/admission"
	"github.com/yourusername/imagesizegatekeeper/pkg/config"
)

var (
	// 命令行参数
	port        = flag.Int("port", 8443, "Webhook服务器端口")
	tlsCertFile = flag.String("tlsCertFile", "/etc/webhook/certs/tls.crt", "TLS证书文件路径")
	tlsKeyFile  = flag.String("tlsKeyFile", "/etc/webhook/certs/tls.key", "TLS私钥文件路径")
	configFile  = flag.String("configFile", "/etc/webhook/config/config.json", "配置文件路径")
	secretsDir  = flag.String("secretsDir", "/etc/webhook/secrets", "敏感信息目录路径")
)

// 加载配置文件
func loadConfig(path string, secretsDirPath string) (*config.Config, error) {
	var cfg *config.Config

	// 如果配置文件不存在，创建一个默认配置
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Printf("配置文件 %s 不存在，使用默认配置", path)
		cfg = config.NewConfig()
	} else {
		// 读取配置文件
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置文件失败: %v", err)
		}

		// 解析配置文件
		cfg = &config.Config{}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件失败: %v", err)
		}

		// 编译正则表达式
		for i := range cfg.NamespaceLimits {
			pattern, err := regexp.Compile(cfg.NamespaceLimits[i].Pattern)
			if err != nil {
				return nil, fmt.Errorf("编译正则表达式失败 %s: %v",
					cfg.NamespaceLimits[i].Pattern, err)
			}
			cfg.NamespaceLimits[i].CompiledPattern = pattern
		}
	}

	// 加载敏感信息
	if err := cfg.LoadSecrets(secretsDirPath); err != nil {
		log.Printf("加载敏感信息失败: %v", err)
		// 继续执行，不中断程序
	} else {
		log.Println("成功加载敏感信息")
	}

	return cfg, nil
}

// 健康检查处理程序
func healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func main() {
	// 解析命令行参数
	flag.Parse()

	// 加载配置
	cfg, err := loadConfig(*configFile, *secretsDir)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建Webhook处理程序
	webhook := admission.NewImageSizeWebhook(cfg)

	// 创建HTTP路由
	mux := http.NewServeMux()
	mux.Handle("/validate", webhook)
	mux.HandleFunc("/healthz", healthz)

	// 创建HTTP服务器
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// 启动HTTP服务器（在goroutine中）
	go func() {
		log.Printf("启动服务器，监听端口 %d", *port)
		if err := server.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile); err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("服务器异常退出: %v", err)
			}
		}
	}()

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Printf("接收到信号: %v，正在关闭服务器", sig)

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("服务器关闭失败: %v", err)
	}

	log.Println("服务器已安全关闭")
}
