#!/bin/bash

set -e

NAMESPACE="imagesizegatekeeper"

# 确保命名空间存在
kubectl get namespace $NAMESPACE > /dev/null 2>&1 || kubectl create namespace $NAMESPACE

# 应用配置
echo "正在部署V2Ray代理..."
kubectl apply -f $(dirname "$0")/deployment.yaml
kubectl apply -f $(dirname "$0")/service.yaml

echo "V2Ray代理部署完成，服务在集群内可通过 v2ray-proxy.${NAMESPACE}.svc.cluster.local 访问"
echo "Socks代理端口: 1080"
echo "HTTP代理端口: 8080"
echo "代理URL示例:"
echo "  - HTTP代理: http://v2ray-proxy.${NAMESPACE}.svc.cluster.local:8080"
echo "  - Socks代理: socks5://v2ray-proxy.${NAMESPACE}.svc.cluster.local:1080" 