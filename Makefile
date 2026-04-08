APP_NAME := jucobot
BIN_DIR := bin
CONFIG ?= configs/config.yaml
DEPLOY_TARGET ?=
RELEASE_ID ?=

TF_DIR := infra/terraform

.DEFAULT_GOAL := help

.PHONY: help build build-arm64 run dev test lint tidy verify e2e install-hooks package-release deploy-remote deploy-validate deploy-history full-deploy pull-secrets remote-bootstrap remote-up-redroid remote-up-jucobot remote-rollback sonar-main quality-gate tf-init tf-plan tf-apply tf-destroy tf-output deploy-aws aws-up-jucobot

# --- Development ---

help: ## 사용 가능한 Make 타겟 목록 표시
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mUsage:\033[0m make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_-]+:.*##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

build: ## 바이너리 빌드 (로컬 아키텍처)
	CGO_ENABLED=0 go build -o $(BIN_DIR)/$(APP_NAME) ./cmd/jucobot

build-arm64: ## linux/arm64 크로스 빌드
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(APP_NAME)-arm64 ./cmd/jucobot

run: ## 운영 설정으로 실행
	go run ./cmd/jucobot --config $(CONFIG)

dev: ## 로컬 개발 서버 (HTTP test transport, :18080)
	go run ./cmd/jucobot --config configs/config.dev.yaml

tidy: ## go mod tidy
	go mod tidy

# --- Testing ---

test: ## 전체 유닛 테스트
	go test ./...

lint: ## 정적 분석 (go vet)
	go vet ./...

verify: ## 로컬 검증 (test + race + shuffle + coverage)
	./scripts/verify/local-verify.sh

e2e: ## E2E 통합 테스트
	go test ./tests/e2e/ -v -count=1 -timeout=120s

sonar-main: ## SonarQube 분석 (main 브랜치)
	./scripts/verify/sonar-main.sh

quality-gate: ## SonarQube 품질 게이트 검증
	./scripts/verify/quality-gate.sh

install-hooks: ## pre-push git 훅 설치
	./scripts/git-hooks/install.sh

# --- Deployment ---

package-release: ## 릴리스 아카이브 생성
	./scripts/deploy/package-release.sh

deploy-validate: ## 원격 서버 배포 사전검증
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/validate-env-remote.sh

deploy-remote: ## 원격 서버에 릴리스 배포
	DEPLOY_TARGET="$(DEPLOY_TARGET)" RELEASE_ID="$(RELEASE_ID)" ./scripts/deploy/deploy-remote.sh

full-deploy: ## 통합 배포 (deploy → bootstrap → redroid → android → jucobot)
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/full-deploy.sh

remote-bootstrap: ## 원격 서버 호스트 초기화 (binder, binderfs)
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/remote-bootstrap.sh

remote-up-redroid: ## 원격 Redroid 컨테이너 시작
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/remote-up-redroid.sh

remote-up-jucobot: ## 원격 JucoBot 컨테이너 시작
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/remote-up-jucobot.sh

remote-rollback: ## 원격 서버 이전 릴리스로 롤백
	DEPLOY_TARGET="$(DEPLOY_TARGET)" RELEASE_ID="$(RELEASE_ID)" ./scripts/deploy/remote-rollback.sh

deploy-history: ## 최근 배포 이력 조회 (최근 10건)
	@DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/deploy-history.sh

pull-secrets: ## AWS SSM에서 시크릿 가져오기
	DEPLOY_TARGET="$(DEPLOY_TARGET)" ./scripts/deploy/pull-secrets-remote.sh

# --- Infrastructure ---

tf-init: ## Terraform 초기화
	cd $(TF_DIR) && terraform init

tf-plan: ## Terraform 변경 계획 미리보기
	cd $(TF_DIR) && terraform plan

tf-apply: ## Terraform 인프라 적용
	cd $(TF_DIR) && terraform apply

tf-destroy: ## Terraform 인프라 삭제
	cd $(TF_DIR) && terraform destroy

tf-output: ## Terraform 출력값 조회
	cd $(TF_DIR) && terraform output

deploy-aws: ## AWS 배포 (Terraform 출력 기반)
	$(MAKE) deploy-remote DEPLOY_TARGET=$$(cd $(TF_DIR) && terraform output -raw deploy_target)

aws-up-jucobot: ## AWS JucoBot 시작 (Terraform 출력 기반)
	$(MAKE) remote-up-jucobot DEPLOY_TARGET=$$(cd $(TF_DIR) && terraform output -raw deploy_target)
