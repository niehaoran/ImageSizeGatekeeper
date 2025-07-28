#!/bin/bash
set -e

# 设置工作目录
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd ${DIR}

# 创建命名空间
kubectl apply -f namespace.yaml

# 应用ConfigMap
kubectl apply -f config.yaml

# 生成证书并创建Secret
./gen-certs.sh

# 获取CA证书
CA_BUNDLE=$(kubectl get secret webhook-tls -n image-size-gatekeeper -o jsonpath="{.data.ca\.crt}")

# 替换webhook.yaml中的CA证书
sed "s/\${CA_BUNDLE}/${CA_BUNDLE}/g" webhook.yaml > webhook-ca.yaml

# 部署应用
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f webhook-ca.yaml

echo "部署完成！" 