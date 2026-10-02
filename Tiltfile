# Ambiente local: `tilt up` sobe Postgres, Redis, MinIO, migrations, API e worker
# num cluster kind/k3d. Requer um cluster local ativo no contexto do kubectl.
allow_k8s_contexts(['kind-parceiros', 'k3d-parceiros'])

docker_build('parceiros', 'backend', dockerfile='backend/Dockerfile')

k8s_yaml(kustomize('deploy/overlays/dev'))

k8s_resource('postgres', labels=['infra'])
k8s_resource('redis', labels=['infra'])
k8s_resource('minio', port_forwards=['9001:9001'], labels=['infra'])
k8s_resource('parceiros-migrate', resource_deps=['postgres'], labels=['app'])
k8s_resource('parceiros-api', port_forwards=['8080:8080'],
             resource_deps=['parceiros-migrate', 'redis'], labels=['app'])
k8s_resource('parceiros-worker', resource_deps=['parceiros-migrate'], labels=['app'])
