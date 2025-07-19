#!/bin/bash

# 参数
SERVICE=imagesizegatekeeper
NAMESPACE=imagesizegatekeeper
TEMP_DIR=/tmp

# 创建命名空间（如果不存在）
kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -

echo "生成 Webhook TLS 证书..."

# 创建证书签名请求配置
cat << EOF > ${TEMP_DIR}/csr.conf
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF

# 生成CA证书和私钥
openssl genrsa -out ${TEMP_DIR}/ca.key 2048
openssl req -x509 -new -nodes -key ${TEMP_DIR}/ca.key -days 3650 -out ${TEMP_DIR}/ca.crt -subj "/CN=image-size-gatekeeper-ca"

# 生成服务器证书和私钥
openssl genrsa -out ${TEMP_DIR}/server.key 2048
openssl req -new -key ${TEMP_DIR}/server.key -out ${TEMP_DIR}/server.csr -subj "/CN=${SERVICE}.${NAMESPACE}.svc" -config ${TEMP_DIR}/csr.conf
openssl x509 -req -in ${TEMP_DIR}/server.csr -CA ${TEMP_DIR}/ca.crt -CAkey ${TEMP_DIR}/ca.key -CAcreateserial -out ${TEMP_DIR}/server.crt -days 3650 -extensions v3_req -extfile ${TEMP_DIR}/csr.conf

echo "创建 Secret 存储证书..."

# 创建secret
kubectl create secret generic imagesizegatekeeper-certs \
        --from-file=tls.key=${TEMP_DIR}/server.key \
        --from-file=tls.crt=${TEMP_DIR}/server.crt \
        --dry-run=client -o yaml |
    kubectl -n ${NAMESPACE} apply -f -

# 更新webhook配置文件中的CA_BUNDLE
echo "更新 ValidatingWebhookConfiguration 中的 CA_BUNDLE..."

CA_BUNDLE=$(base64 -w 0 ${TEMP_DIR}/ca.crt)
TEMP_WEBHOOK_CONFIG=${TEMP_DIR}/webhook-patched.yaml

# 复制webhook配置文件
cp deploy/webhook.yaml ${TEMP_WEBHOOK_CONFIG}

# 替换CA_BUNDLE占位符
# 首先，移除cert-manager相关的注解
sed -i '/cert-manager\.io\/inject-ca-from/d' ${TEMP_WEBHOOK_CONFIG}

# 然后，添加caBundle
# 如果webhook.yaml文件中有caBundle行，则替换它
# 如果没有，则在clientConfig节后面添加它
if grep -q "caBundle:" ${TEMP_WEBHOOK_CONFIG}; then
    # 替换现有的caBundle行
    sed -i "s|caBundle:.*|caBundle: ${CA_BUNDLE}|" ${TEMP_WEBHOOK_CONFIG}
else
    # 在clientConfig节后添加caBundle
    sed -i "/clientConfig:/a\\      caBundle: ${CA_BUNDLE}" ${TEMP_WEBHOOK_CONFIG}
fi

# 应用webhook配置
kubectl apply -f ${TEMP_WEBHOOK_CONFIG}

echo "清理临时文件..."

# 清理临时文件
rm -f ${TEMP_DIR}/csr.conf ${TEMP_DIR}/ca.* ${TEMP_DIR}/server.* ${TEMP_WEBHOOK_CONFIG}

echo "证书生成和配置完成!" 