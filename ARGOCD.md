# To login
## Add an argo API account
Add the following to the `argo-cm` configmap `data` section
```shell
accounts.argo-api-user: apiKey, login
```

## Set the argo rbac policy for the new account
Add the `policy.csv` section to the `argocd-rbac-cm` configmap
```shell
data:
  policy.csv: |
    g, argo-api-user, role:admin
```

## Get admin password
```shell
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d; echo
```
```shell
argocd login localhost:8080 --insecure
Username: admin
Password: <password_from_above>
```

## Generate account token for API usage
```shell
argocd account generate-token --account argo-api-user
```