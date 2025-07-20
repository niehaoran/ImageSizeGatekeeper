#!/bin/bash

set -e

NAMESPACE="imagesizegatekeeper"
DEPLOY_PROXY=false

# 解析参数
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --deploy-proxy) DEPLOY_PROXY=true; shift ;;
        *) echo "未知选项: $1"; exit 1 ;;
    esac
done

# 创建命名空间（如果不存在）
kubectl get namespace $NAMESPACE > /dev/null 2>&1 || kubectl create namespace $NAMESPACE

echo "正在部署 ImageSizeGatekeeper..."

# 部署基础组件
kubectl apply -f $(dirname "$0")/namespace.yaml
kubectl apply -f $(dirname "$0")/config.yaml
kubectl apply -f $(dirname "$0")/secrets.yaml
kubectl apply -f $(dirname "$0")/self-signed-issuer.yaml
kubectl apply -f $(dirname "$0")/certificate.yaml
kubectl apply -f $(dirname "$0")/deployment.yaml
kubectl apply -f $(dirname "$0")/service.yaml
kubectl apply -f $(dirname "$0")/webhook.yaml

# 如果需要部署代理，则部署代理组件
if [ "$DEPLOY_PROXY" = true ]; then
    echo "正在部署 V2Ray 代理..."
    
    # 检查代理部署脚本是否存在
    if [ -f "$(dirname "$0")/proxy/deploy.sh" ]; then
        bash "$(dirname "$0")/proxy/deploy.sh"
    else
        echo "错误：找不到代理部署脚本 $(dirname "$0")/proxy/deploy.sh"
        exit 1
    fi
    
    echo "V2Ray 代理部署完成"
fi

echo "ImageSizeGatekeeper 部署完成!"
echo "可以使用以下命令查看Pod状态:"
echo "  kubectl get pods -n $NAMESPACE" 