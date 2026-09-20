# Demo

## 1. Select an existing cluster

```powershell
kubectl cluster-info
kubectl get nodes -o wide
kubectl get deployments -A
```

Choose a safe existing deployment. Do not modify production workloads.

## 2. Inspect the workload

```powershell
kubectl get deployment <name> -n <namespace> -o yaml
kubectl get replicasets -n <namespace> -o wide
kubectl get pods -n <namespace> -o wide
```

## 3. Run the CLI

```powershell
cd C:\Users\prasu\Downloads\cli-hunt\k8s-truth
C:\tools\go\bin\go.exe build -o stateproof.exe .
.\stateproof.exe verify deployment <name> -n <namespace>
.\stateproof.exe explain deployment <name> -n <namespace>
.\stateproof.exe evidence deployment <name> -n <namespace> --json
.\stateproof.exe scan <namespace>
```

## 4. Trigger divergence

```powershell
Only test divergence on a temporary isolated workload that you created for this purpose. Do not mutate an existing workload.
```

## 5. Observe evidence

```powershell
kubectl get deployment demo -o yaml
kubectl get replicasets -o wide
kubectl get pods -o wide
.\truth.exe evidence deployment demo -n default --json
```

## 6. Reconcile and verify again

```powershell
kubectl rollout status deployment/<name> -n <namespace> --timeout=180s
.\stateproof.exe verify deployment <name> -n <namespace>
```
