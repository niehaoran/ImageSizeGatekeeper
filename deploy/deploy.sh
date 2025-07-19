#!/bin/bash

# 设置命名空间
NAMESPACE=imagesizegatekeeper

# 创建命名空间
kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -

# 部署基础资源
echo "部署基础资源..."
kubectl apply -f deploy/namespace.yaml
kubectl apply -f deploy/config.yaml
kubectl apply -f deploy/secrets.yaml

# 创建自签名CA Issuer
echo "创建自签名CA Issuer..."
kubectl apply -f deploy/self-signed-issuer.yaml

# 等待CA Issuer就绪
echo "等待CA Issuer就绪..."
sleep 5
kubectl wait --for=condition=Ready --timeout=60s clusterissuer webhook-ca-issuer

# 部署证书资源
echo "配置证书资源..."
kubectl apply -f deploy/certificate.yaml

# 等待证书生成
echo "等待证书生成..."
kubectl wait --for=condition=Ready --timeout=60s certificate imagesizegatekeeper-cert -n ${NAMESPACE}

# 部署应用程序资源
echo "部署应用程序..."
kubectl apply -f deploy/deployment.yaml
kubectl apply -f deploy/service.yaml

# 部署Webhook配置
echo "部署Webhook配置..."
kubectl apply -f deploy/webhook.yaml

# 查看部署状态
echo "等待部署完成..."
sleep 5
kubectl get pods -n ${NAMESPACE}

# 检查证书状态
echo "检查证书状态..."
kubectl get certificate -n ${NAMESPACE}

echo "部署完成！请确保Pod已成功运行且证书已就绪。" 