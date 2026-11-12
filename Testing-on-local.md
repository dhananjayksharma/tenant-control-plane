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



## Test report:

export AMQP_URL="$(
  minikube -p gpu-cpu-lab kubectl -- \
    get secret control-plane-credentials \
    -n tenant-control-plane \
    -o jsonpath='{.data.amqp-url}' | base64 -d
)"

export AMQP_URL="${AMQP_URL/@rabbitmq:5672\//@127.0.0.1:5672/}"

minikube -p gpu-cpu-lab kubectl -- \
  -n tenant-control-plane \
  port-forward svc/rabbitmq 5672:5672
  
scripts/test.sh
?       github.com/dhananjayksharma/tenant-control-plane/cmd/api        [no test files]
?       github.com/dhananjayksharma/tenant-control-plane/cmd/worker     [no test files]
?       github.com/dhananjayksharma/tenant-control-plane/internal/broker        [no test files]
ok      github.com/dhananjayksharma/tenant-control-plane/internal/core  1.018s
ok      github.com/dhananjayksharma/tenant-control-plane/internal/httpapi       1.017s
?       github.com/dhananjayksharma/tenant-control-plane/internal/messaging     [no test files]
?       github.com/dhananjayksharma/tenant-control-plane/internal/store [no test files]
?       github.com/dhananjayksharma/tenant-control-plane/scripts        [no test files]
=== RUN   TestFullLifecycle
--- PASS: TestFullLifecycle (2.91s)
=== RUN   TestConcurrentPatchSameVersion
--- PASS: TestConcurrentPatchSameVersion (0.63s)
=== RUN   TestConcurrentCreateSlug
--- PASS: TestConcurrentCreateSlug (0.00s)
=== RUN   TestValidationAndNotFound
--- PASS: TestValidationAndNotFound (0.00s)
=== RUN   TestDuplicateAndStaleProgress
--- PASS: TestDuplicateAndStaleProgress (3.90s)
PASS
ok      github.com/dhananjayksharma/tenant-control-plane/tests  8.466s


curl -sS -X POST http://localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"slug":"sbi-test-01","name":"SBI Test Demo"}'
