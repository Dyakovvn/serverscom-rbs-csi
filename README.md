# RBS CSI Driver

Kubernetes CSI driver for Remote Block Storage (RBS) - dynamic provisioning and management of iSCSI block volumes.

## Features

- Dynamic volume provisioning via RBS API
- iSCSI block device attachment
- Online volume expansion
- Multiple filesystems: ext4, ext3, xfs, btrfs
- CHAP authentication
- **PVC naming** - volumes named by PVC instead of UUID
- **Label propagation** - StorageClass → PVC → RBS volume labels
- **Idempotent operations** - safe retries via label lookup

## Quick Start

```bash
# 1. Create credentials secret
kubectl create secret generic rbs-csi-secret \
  --from-literal=api-url="https://api.servers.com/v1" \
  --from-literal=api-token="your-api-token" \
  --namespace=kube-system

# 2. Deploy latest driver version
kubectl apply -f https://github.com/serverscom/serverscom-rbs-csi/releases/latest/download/rbs-csi-deploy.yaml

# or specific version
kubectl apply -f https://github.com/serverscom/serverscom-rbs-csi/releases/download/v0.1.0/rbs-csi-deploy.yaml

# 3. Create StorageClass
kubectl apply -f examples/storageclass.yaml

# 4. Create PVC
kubectl apply -f examples/pvc.yaml
```

## Configuration

### StorageClass Parameters

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: rbs-ssd
  labels:
    environment: production
provisioner: rbs.csi.servers.com
parameters:
  rbs.csi.servers.com/location: "40"
  rbs.csi.servers.com/flavor: "16997"
  # names for location and flavor also supported
  # rbs.csi.servers.com/flavor: "SSD-High" 
  # rbs.csi.servers.com/location: "AMS1"
  rbs.csi.servers.com/labels: |
    {
      "managed-by": "kubernetes"
    }
allowVolumeExpansion: true
volumeBindingMode: WaitForFirstConsumer
```

## Label Propagation

Labels merge with priority: **System > PVC > StorageClass**

Example:
```yaml
# StorageClass labels
parameters:
  rbs.csi.servers.com/labels: |
    {
      "managed-by": "kubernetes"
    }

---
# PVC labels
metadata:
  labels:
    app: database
    environment: staging  # Overrides StorageClass

# Result on RBS volume:
# {
#   "pvc-uuid": "abc-123",
#   "pvc-namespace": "default",
#   "app": "database",
#   "environment": "staging",
#   "managed-by": "kubernetes"
# }
```

## Architecture

**CSI Controller** (Deployment):
- CreateVolume, DeleteVolume, ExpandVolume
- RBS API integration

**CSI Node** (DaemonSet):
- StageVolume, UnstageVolume
- iSCSI discovery and login
- Filesystem formatting and mounting

## Examples

### Basic PVC

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-data
  labels:
    app: nginx
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: rbs-ssd
  resources:
    requests:
      storage: 10Gi
```

### Volume Expansion

```yaml
spec:
  resources:
    requests:
      storage: 20Gi  # Increased from 10Gi
```


## License

Apache 2.0
