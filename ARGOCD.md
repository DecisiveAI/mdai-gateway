# To login
## Add an argo API account
Directly edit the `argo-cm` configmap `data` section with the following appended:
```shell
accounts.apiUser: apiKey
```

OR

```shell
kubectl patch configmap/argocd-cm --type merge -p '{"data":{"accounts.apiUser":"apiKey"}}' -n argocd
```

## Set the argo rbac policy for the new account
Add the `policy.csv` section to the `argocd-rbac-cm` configmap
```shell
data:
  policy.csv: |
    g, apiUser, role:admin
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

OR

```shell
argocd login localhost:8080 --insecure --username admin --password '<admin-password>'
```

## Generate account token for API usage (optional expiry of 90 days)
```shell
argocd account generate-token --account apiUser --expires-in 2160h
```