# ImageSizeGatekeeper
限制k8s拉取最大容器大小的Admission Webhook组件

## 功能描述
1. 创建仓库列表，只对列表中仓库拉取的镜像进行大小限制
   - 支持处理加速站的情况，例如docker.1ms.run实际上指向docker.io
2. 对不同命名空间设置不同的镜像大小限制
   - 支持正则表达式匹配命名空间
   - 例如：user-* 表示所有以user-开头的命名空间
3. 支持设置网络代理，用于国内环境请求Docker Registry API
   - 支持HTTP和SOCKS5代理
   - 内置V2Ray代理，支持VLESS协议，适用于国内环境

## 快速开始

### 前置条件
- Kubernetes集群
- kubectl命令行工具
- cert-manager（用于自动管理证书）

### 部署步骤

1. 克隆代码库
```bash
git clone https://github.com/yourusername/imagesizegatekeeper.git
cd imagesizegatekeeper
```

2. 修改配置
编辑 `deploy/config.yaml` 文件，根据需要配置：
- 仓库映射关系
- 命名空间大小限制
- 代理设置

3. 配置证书
首先，我们提供了一个自签名CA Issuer的配置，这在文件`deploy/self-signed-issuer.yaml`中定义。这是为了确保即使没有公网域名，我们也能为内部服务签发证书。

如果您的集群已经有可用于内部服务的Issuer，您可以在`deploy/certificate.yaml`中修改`issuerRef`：
```yaml
issuerRef:
  name: webhook-ca-issuer  # 修改为您环境中可用的Issuer
  kind: ClusterIssuer
  group: cert-manager.io
```

4. 配置敏感信息
编辑 `deploy/secrets.yaml` 文件，设置：
- 代理URL和类型
- 仓库认证信息

5. 构建和推送Docker镜像
```bash
docker build -t your-registry/imagesizegatekeeper:latest .
docker push your-registry/imagesizegatekeeper:latest
```

6. 修改部署文件中的镜像地址
编辑 `deploy/deployment.yaml`，将 `image: imagesizegatekeeper:latest` 替换为你的镜像地址。

7. 部署所有资源

不带代理部署：
```bash
chmod +x deploy/deploy.sh
./deploy/deploy.sh
```

带V2Ray代理部署：
```bash
chmod +x deploy/deploy.sh
./deploy/deploy.sh --deploy-proxy
```

这个脚本会自动执行以下操作：
- 创建所需的命名空间
- 部署cert-manager证书资源
- 部署Webhook服务和相关资源
- 配置ValidatingWebhookConfiguration
- 如果指定了 --deploy-proxy，则部署V2Ray代理

## 测试验证

1. 创建一个测试Pod，使用小于限制大小的镜像
```bash
kubectl create -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-small-image
  namespace: default
spec:
  containers:
  - name: nginx
    image: nginx:alpine
EOF
```

2. 创建一个测试Pod，使用大于限制大小的镜像
```bash
kubectl create -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-large-image
  namespace: default
spec:
  containers:
  - name: heavy-image
    image: tensorflow/tensorflow:latest
EOF
```

## 配置说明

### 仓库映射
配置镜像仓库的映射关系，处理加速站的情况。
```yaml
registryMappings:
  - userRegistry: "docker.io"
    actualRegistry: "registry-1.docker.io"
  - userRegistry: "gcr.io"
    actualRegistry: "gcr.io"
```

### 命名空间限制
为不同命名空间设置镜像大小限制。
```yaml
namespaceLimits:
  - pattern: "^prod-.*$"
    maxSizeGB: 5.0
  - pattern: "^dev-.*$"
    maxSizeGB: 10.0
  - pattern: ".*"
    maxSizeGB: 8.0
```

### 代理配置
设置是否使用代理以及代理的类型。
```yaml
proxy:
  enabled: false
  type: "none"  # 可选值: "none", "http", "socks5"
  url: ""
```

### V2Ray代理配置
项目内置了V2Ray代理功能，支持VLESS协议。配置文件位于`deploy/proxy/v2ray-config.json`：
```json
{
  "inbounds": [
    {
      "port": 1080,
      "protocol": "socks",
      "settings": {
        "auth": "noauth",
        "udp": true
      }
    },
    {
      "port": 8080,
      "protocol": "http"
    }
  ],
  "outbounds": [
    {
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "your-server-address",
            "port": 443,
            "users": [
              {
                "id": "your-uuid",
                "encryption": "none"
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "grpc",
        "security": "reality",
        "realitySettings": {
          "fingerprint": "chrome",
          "serverName": "your-sni",
          "publicKey": "your-public-key",
          "shortId": "your-short-id"
        }
      }
    }
  ]
}
```

### 敏感信息配置
所有敏感信息（如仓库认证信息和代理URL）都通过Kubernetes Secret进行管理，而不是直接在ConfigMap中配置。

1. 代理配置
在Secret中添加以下键:
- `proxy-url`: 代理服务器URL
- `proxy-type`: 代理类型 (http, socks5)

2. 仓库认证信息
在Secret中添加格式为`registry_credentials_<registry>`的键，值为`<username>:<password>`。

例如：
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: imagesizegatekeeper-secrets
  namespace: imagesizegatekeeper
type: Opaque
stringData:
  # 代理配置
  proxy-url: "socks5://v2ray-proxy.imagesizegatekeeper.svc.cluster.local:1080"
  proxy-type: "socks5"
  
  # 仓库认证信息
  registry_credentials_docker.io: "username:password"
  registry_credentials_ghcr.io: "github-username:github-token"
```
