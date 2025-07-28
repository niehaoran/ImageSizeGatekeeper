# ImageSizeGatekeeper
限制k8s拉取最大容器大小的Admission Webhook组件

## 项目功能简介
1. 能够限制指定命名空间拉取镜像的最大容量 - 会先获取信息查看预估大小，超过最大限制不允许拉取。查看镜像获取大小失败也不允许拉取
2. 对于限制的命名空间部署镜像需要在yaml添加指定的一个字段来指定当前镜像仓库的原仓库，例如用户使用demo.52013120.xyz加速站来拉镜像，可是他是加速docker的，也就是说它需要额外增加一个字段来指定它本身的仓库是谁，我们的Admission Webhook组件对于仓库就会直接使用【它本身的仓库】，而不是加速站
3. 可以支持挂载代理【通过保密字典或者配置字典】
4. 支持通过正则表达式匹配命名空间，可以灵活控制哪些命名空间需要受限制
5. 支持用户通过Secret指定私有仓库的认证信息

## 实现原理
该组件通过K8s的Admission Webhook机制，在Pod创建前检查其使用的镜像大小。借助skopeo工具，可以在不拉取整个镜像的情况下获取镜像的大小信息，从而实现对镜像大小的控制。

## 部署说明
1. 克隆本仓库
```bash
git clone https://github.com/your-org/ImageSizeGatekeeper.git
cd ImageSizeGatekeeper
```

2. 修改配置
编辑`deploy/config.yaml`，设置需要限制的命名空间和对应的大小限制

3. 执行部署脚本
```bash
cd deploy
./deploy.sh
```

## 配置说明
配置文件示例：
```yaml
# 服务器配置
port: 8443
tlsCertFile: "/etc/webhook/certs/tls.crt"
tlsKeyFile: "/etc/webhook/certs/tls.key"
logLevel: "info"

# 代理配置
proxyEnabled: false
proxyURL: ""  # 例如 "socks5h://username:password@host:port"

# 命名空间限制配置
namespaceRestrictions:
  # 精确匹配的命名空间
  default:
    enabled: true
    maxSizeMB: 1000
    requireOriginalRegistry: true
  
  # 正则表达式匹配的命名空间
  "^user-.*$":
    enabled: true
    maxSizeMB: 300
    requireOriginalRegistry: true
    isRegex: true  # 启用正则表达式匹配
```

### 命名空间限制配置说明
- 精确匹配：直接使用命名空间的名称作为键
- 正则表达式匹配：使用正则表达式作为键，并设置`isRegex: true`
- `enabled`: 是否启用该规则
- `maxSizeMB`: 允许的最大镜像大小（MB）
- `requireOriginalRegistry`: 是否要求指定原始仓库
- `isRegex`: 是否使用正则表达式匹配命名空间

正则表达式示例：
- `^user-.*$`: 匹配所有以"user-"开头的命名空间
- `^(team|project)-.*$`: 匹配所有以"team-"或"project-"开头的命名空间
- `.*-dev$`: 匹配所有以"-dev"结尾的命名空间

## 使用方法

### 基本使用方法
在Pod的yaml文件中添加注解，指定原始仓库：
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  annotations:
    imagesizegatekeeper.k8s.io/original-registry: "docker.io"  # 原始仓库
spec:
  containers:
  - name: nginx
    image: demo.52013120.xyz/nginx:latest
```

### 私有仓库认证
对于需要认证的私有仓库，您可以创建一个Secret并在Pod中引用它：

#### 1. 创建认证Secret
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-registry-credentials
  namespace: your-namespace
type: Opaque
stringData:
  # 方法1: 使用auth.json格式，支持多个仓库
  auth.json: |
    {
      "registries": {
        "docker.io": {
          "username": "your-username",
          "password": "your-password"
        },
        "quay.io": {
          "username": "quay-username",
          "password": "quay-password"
        }
      }
    }
  
  # 方法2: 为每个仓库创建单独的条目
  docker.io: |
    {
      "username": "your-username",
      "password": "your-password"
    }
```

也可以使用kubectl命令创建：
```bash
# 创建auth.json文件
cat > auth.json << EOF
{
  "registries": {
    "docker.io": {
      "username": "your-username",
      "password": "your-password"
    }
  }
}
EOF

# 创建Secret
kubectl create secret generic my-registry-credentials \
  --from-file=auth.json \
  --namespace your-namespace
```

#### 2. 在Pod中引用Secret
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: private-image-pod
  annotations:
    imagesizegatekeeper.k8s.io/original-registry: "docker.io"
    imagesizegatekeeper.k8s.io/credentials-secret: "my-registry-credentials"  # 引用认证Secret
spec:
  containers:
  - name: private-app
    image: private-registry.com/private-app:latest
```

## 构建镜像
```bash
docker build -t your-registry/image-size-gatekeeper:latest .
docker push your-registry/image-size-gatekeeper:latest
```
