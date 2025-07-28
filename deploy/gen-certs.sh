#!/bin/bash

# 设置变量
SERVICE=image-size-gatekeeper
NAMESPACE=image-size-gatekeeper
SECRET_NAME=webhook-tls
TEMP_DIR=$(mktemp -d)

# 创建CA密钥和证书
openssl genrsa -out ${TEMP_DIR}/ca.key 2048
openssl req -x509 -new -nodes -key ${TEMP_DIR}/ca.key -days 3650 -out ${TEMP_DIR}/ca.crt -subj "/CN=admission-webhook-ca"

# 创建服务器密钥
openssl genrsa -out ${TEMP_DIR}/tls.key 2048

# 创建CSR配置
cat > ${TEMP_DIR}/csr.conf <<EOF
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
EOF

# 创建服务器证书请求
openssl req -new -key ${TEMP_DIR}/tls.key -out ${TEMP_DIR}/server.csr -subj "/CN=${SERVICE}.${NAMESPACE}.svc" -config ${TEMP_DIR}/csr.conf

# 签发服务器证书
openssl x509 -req -in ${TEMP_DIR}/server.csr -CA ${TEMP_DIR}/ca.crt -CAkey ${TEMP_DIR}/ca.key \
    -CAcreateserial -out ${TEMP_DIR}/tls.crt -days 3650 \
    -extensions v3_req -extfile ${TEMP_DIR}/csr.conf

# 创建Kubernetes Secret
kubectl create secret generic ${SECRET_NAME} \
    --from-file=tls.crt=${TEMP_DIR}/tls.crt \
    --from-file=tls.key=${TEMP_DIR}/tls.key \
    --from-file=ca.crt=${TEMP_DIR}/ca.crt \
    -n ${NAMESPACE}

# 输出CA证书以便后续使用
echo
echo "CA Certificate:"
cat ${TEMP_DIR}/ca.crt | base64 | tr -d '\n'
echo
echo

# 清理临时文件
rm -rf ${TEMP_DIR} 