# Local environment: `tilt up` starts Postgres, Redis, SeaweedFS (S3), Mailpit (email), migrations, API and
# worker in a kind/k3d cluster. Needs a local cluster active in the kubectl context.
allow_k8s_contexts(['kind-parceiros', 'k3d-parceiros'])

docker_build('parceiros', 'backend', dockerfile='backend/Dockerfile')

k8s_yaml(kustomize('deploy/overlays/dev'))

k8s_resource('postgres', labels=['infra'])
k8s_resource('redis', labels=['infra'])
k8s_resource('seaweedfs', port_forwards=['8333:8333'], labels=['infra'])
k8s_resource('mailpit', port_forwards=['8025:8025'], links=['http://localhost:8025'], labels=['infra'])
k8s_resource('parceiros-migrate', resource_deps=['postgres'], labels=['app'])
k8s_resource('parceiros-api', port_forwards=['8080:8080'],
             resource_deps=['parceiros-migrate', 'redis'], labels=['app'])
k8s_resource('parceiros-worker', resource_deps=['parceiros-migrate'], labels=['app'])

# Front end (Vite) outside the cluster, proxying /v1 to the API at localhost:8080.
local_resource('web', serve_cmd='npm run dev', serve_dir='web',
               cmd='npm ci', dir='web', deps=['web/package-lock.json'],
               links=['http://localhost:5173'], resource_deps=['parceiros-api'], labels=['app'])
