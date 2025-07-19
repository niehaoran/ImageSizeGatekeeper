# ImageSizeGatekeeper
限制k8s拉取最大容器大小的Admission Webhook组件

## 功能描述
1. 创建仓库列表，只对列表中仓库拉取的镜像进行大小限制
   - 支持处理加速站的情况，例如docker.1ms.run实际上指向docker.io
2. 对不同命名空间设置不同的镜像大小限制
   - 支持正则表达式匹配命名空间
   - 例如：user-* 表示所有以user-开头的命名空间
3. 支持设置网络代理，用于国内环境请求Docker Registry API

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
- 代理URL（如需）
- 仓库认证信息

5. 构建和推送Docker镜像
```bash
docker build -t your-registry/imagesizegatekeeper:latest .
docker push your-registry/imagesizegatekeeper:latest
```

6. 修改部署文件中的镜像地址
编辑 `deploy/deployment.yaml`，将 `image: imagesizegatekeeper:latest` 替换为你的镜像地址。

7. 部署所有资源
```bash
chmod +x deploy/deploy.sh
./deploy/deploy.sh
```

这个脚本会自动执行以下操作：
- 创建所需的命名空间
- 部署cert-manager证书资源
- 等待证书生成完成
- 部署Webhook服务和相关资源
- 配置ValidatingWebhookConfiguration

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
```json
"registryMappings": [
  {
    "userRegistry": "docker.1ms.run",
    "actualRegistry": "docker.io"
  }
]
```

### 命名空间限制
为不同命名空间设置镜像大小限制。
```json
"namespaceLimits": [
  {
    "pattern": "default",
    "maxSizeGB": 10
  },
  {
    "pattern": "user-.*",
    "maxSizeGB": 5
  }
]
```

### 敏感信息配置
所有敏感信息（如仓库认证信息和代理URL）都通过Kubernetes Secret进行管理，而不是直接在ConfigMap中配置。

1. 代理URL配置
在Secret中添加名为`proxy-url`的键，值为代理URL。

2. 仓库认证信息
在Secret中添加格式为`registry-credentials/<registry>`的键，值为`<username>:<password>`。

例如：
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: imagesizegatekeeper-secrets
  namespace: imagesizegatekeeper
type: Opaque
stringData:
  # 代理URL
  proxy-url: "your-proxy-url"
  
  # Docker Hub认证信息
  "registry-credentials/docker.io": "username:password"
  
  # GCR认证信息
  "registry-credentials/gcr.io": "oauth2accesstoken:token"
```

部署Secret:
```bash
kubectl apply -f deploy/secrets.yaml
```

这样设计的好处是：
1. 敏感信息与普通配置分离，提高安全性
2. Secret可以使用Kubernetes的加密机制保护
3. 可以单独更新Secret，而不影响其他配置
