curl -fsS http://localhost:8080/healthz
curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"minikube-demo-01","name":"Minikube Demo"}'
# Inspect tenant/task identifiers returned by POST:
curl -sS 'http://localhost:8080/v1/tenants'
curl -sS 'http://localhost:8080/v1/tasks'




kubectl --context gpu-cpu-lab -n tenant-control-plane  port-forward svc/postgres 15432:5432



curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"mirantis-test-02","name":"Mirantis Test Demo"}'

minikube -p gpu-cpu-lab kubectl -- \
  -n tenant-control-plane \
  port-forward svc/rabbitmq 15672:15672


curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"kpit-test-03","name":"KPIT Test Demo"}'


curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"kpit-test-04","name":"KPIT Test Demo-failed"}'


curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"lti-test-01","name":"LTI Test Demo"}'


curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"sbi-test-01","name":"SBI Test Demo"}'