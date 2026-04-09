# JucoBot v2

JucoBot v2는 Go로 작성된 카카오톡 비공식 봇입니다. Iris를 통해 카카오톡 메시지를 수신하고, app bootstrap, intent catalog, `chat_id` 기반 ACL, 쿠팡 domain/service 경계를 분리한 구조를 제공합니다.

## 운영 호스트 역할

실운영은 호스트가 분리되어 있다.

- `heart@172.30.1.26`: Redroid, Iris, ADB, `scrcpy`
- `ubuntu@3.34.175.209`: JucoBot app, chart-renderer

즉 `adb connect`, `scrcpy`, `make remote-up-redroid`는 `heart@172.30.1.26` 기준이고, JucoBot 앱 배포/재기동은 `ubuntu@3.34.175.209` 기준이다.

## 아키텍처

```text
+-------------------+        +-----------------------+
| KakaoTalk / Iris  | <----> | internal/transport    |
| WebSocket + HTTP  |        | iris.Client           |
+-------------------+        +----------+------------+
                                         |
                                         v
                               +---------+---------+
                               | internal/app      |
                               | bootstrap/runtime |
                               +---------+---------+
                                         |
               +-------------------------+-------------------------+
               v                         v                         v
      +--------+--------+       +--------+--------+       +--------+--------+
      | internal/bot    |       | internal/coupang|       | internal/store  |
      | router/policy   |       | domain/service  |       | adapters        |
      +--------+--------+       +--------+--------+       +--------+--------+
               |                         |                         |
               v                         v                         v
      +--------+--------+       +--------+--------+       +--------+--------+
      | internal/intent |       | internal/scraper|       | pkg/formatter   |
      | catalog         |       | providers/cache |       |                  |
      +-----------------+       +-----------------+       +------------------+
```

## 디렉터리

```text
cmd/jucobot/           앱 진입점
internal/app/          bootstrap, runtime lifecycle
internal/config/       도메인별 설정 로드 및 검증
internal/intent/       canonical intent catalog
internal/bot/          router, policy, middleware, registry
internal/command/      명령어 핸들러
internal/coupang/      쿠팡 domain model/service
internal/scraper/      외부 provider, 캐시, watchlist
internal/store/        generic store + SQLite adapter
pkg/formatter/         메시지 포매팅 유틸리티
deployments/           Dockerfile, compose 파일
configs/               기본 YAML 설정
```

## 빠른 시작

1. Go 1.25.5 이상을 설치합니다.
2. 의존성을 정리합니다.

```bash
make tidy
```

3. 기본 설정을 검토합니다.

```bash
cat configs/config.yaml
```

4. 로컬에서 실행합니다.

```bash
make run
```

## 설정

기본 설정은 `configs/config.yaml`에 있습니다. Docker/운영 환경에서는 `JUCOBOT_` 접두사의 환경변수로 오버라이드할 수 있습니다.

예시:

```bash
export JUCOBOT_IRIS_WS_URL=ws://redroid:3000/ws
export JUCOBOT_IRIS_HTTP_URL=http://redroid:3000
export JUCOBOT_BOT_LOG_LEVEL=debug
```

Kakao transport adapter 없이 코어 런타임만 검증하려면 아래처럼 Iris adapter를 꺼둘 수 있습니다.

```bash
export JUCOBOT_IRIS_ENABLED=false
```

### Slashless 조회 정책

기본 정책은 `explicit-only` 입니다. 일반 잡담은 무시하고, 아래처럼 조회 의도가 드러나는 입력만 처리합니다.

- `비트`
- `BTC`
- `삼전`
- `삼성전자`
- 쿠팡 URL

`explicit-only`는 bare query처럼 조회어로 보이는 입력만 처리합니다. `local-auto`는 여기에 더해 `오늘 비트`, `오늘 삼전` 같은 짧은 alias성 문장도 자동응답할 수 있게 승격한 방입니다.

### Intent Catalog 기준

router, ACL, 도움말, auto-query는 모두 canonical intent ID를 공유합니다. 운영 설정의 `access.rooms[].allow_intents`에는 `help`, `coin`, `stock`, `coupang` 같은 canonical ID를 넣는 것을 기준으로 합니다. 기존 별칭(`help.show`, `coin.quote`)도 정규화되지만, 새 설정과 문서는 canonical ID를 기준으로 유지하는 편이 drift를 줄입니다.

명령의 표시 이름, slash alias, explicit alias, help 예시, ACL normalize key는 하나의 catalog-backed descriptor 계약에서 같이 파생됩니다. `관리 현황` 같은 documented explicit 명령도 이 단일 계약에 등록되어야만 라우팅되며, handler와 catalog가 어긋나면 시작 시 validation 오류로 실패합니다.

### 지원 Store Driver

generic `store.driver`는 현재 `memory`만 지원합니다. 쿠팡 가격 이력은 별도의 SQLite adapter(`coupang.db`)를 사용하며, `store.driver=supabase` 같은 미구현 backend는 기본 지원 목록에서 제외되었습니다.

### 쿠팡 추적 정책

쿠팡 가격 추적은 `DB-first + stale 응답 + tiered refresh` 모델 위에 `Fallcent 매핑 캐시 + 제한된 보조 폴백`을 얹어 동작합니다. 첫 조회는 `검색 -> 후보 검증 -> 매핑 저장` 비용이 들고, 이후 조회와 refresh는 저장된 Fallcent 식별자를 우선 사용합니다.

- `coupang.hot_interval`, `warm_interval`, `cold_interval`: 상품 등급별 refresh 주기
- `coupang.freshness`: 이 시간 안의 스냅샷은 최신으로 간주
- `coupang.read_refresh_timeout`: stale 조회에서 동기 read-through refresh를 시도할 최대 대기 시간
- `coupang.stale_threshold`: 오래된 데이터를 stale로 강조하는 기준
- `coupang.refresh_budget_per_hour`: 시간당 허용할 정기/on-demand refresh 상한
- `coupang.registration_budget_per_hour`: 시간당 신규 상품 등록 상한
- `coupang.resolution_budget_per_hour`: 신규 등록 또는 `needs_recheck` 상품의 Fallcent 검색/후보 검증 상한
- `coupang.registration_latency_budget`: 미추적 첫 조회에서 동기 registration 경로가 사용할 최대 대기 시간
- `coupang.candidate_fanout`: Fallcent 검색 결과에서 상세 검증할 최대 후보 수
- `coupang.mapping_recheck_backoff`: 매핑이 흔들린 상품을 다시 검색하기 전 최소 대기 시간
- `coupang.allow_auxiliary_fallback`: Fallcent 실패 시 쿠팡 보조 provider를 title rescue/최후 폴백으로 허용할지 여부
- `coupang.lookup_coalescing_enabled`: 동일 `track_id` 동시 lookup에서 등록/read-through 작업을 단일 flight로 합류 처리할지 여부
- `coupang.registration_join_wait`: 신규 등록 follower가 leader 결과를 기다릴 최대 시간
- `coupang.read_refresh_join_wait`: stale read-through follower가 leader refresh를 기다릴 최대 시간
- `coupang.tier_window`, `hot_threshold`, `warm_threshold`: 최근 조회량에 따른 hot/warm/cold 승격 기준
- `iris.room_worker_enabled`: inbound 처리에서 room-aware worker(방 간 병렬/방 내 순서 보장) 활성화 여부
- `iris.room_worker_count`: 동시에 처리할 room worker 상한

운영 메모:

- 신규 상품 응답이 비싼 이유는 Fallcent direct mapping이 없어서 검색과 구매 링크 역검증이 필요하기 때문입니다.
- 쿠팡 가격 응답은 기본적으로 상품명과 `현재가`, `최저가`, `최저가 대비`만 간결하게 보여주며, stale 응답일 때만 `최근 확인` 한 줄을 덧붙입니다.
- 신규 상품 첫 조회는 `registration_latency_budget` 안에서 현재가와 최소 bootstrap만 확보하고, chart seed나 rescue enrichment는 비동기로 이어질 수 있습니다. 이 경우 응답 마지막에 `⏳ 가격 이력 보강 중`이 붙습니다.
- tracked 상품이 stale 상태면 먼저 `read_refresh_timeout` 안에서 검증된 Fallcent 매핑 기반 최신값 확인을 시도하고, timeout/실패 시에만 마지막 관측값과 background refresh 상태를 함께 반환합니다.
- 프로세스 시작 직후 watchlist가 freshness를 벗어난 상품을 한 번 warmup 해 재기동 직후 stale-first 구간을 줄입니다.
- Fallcent 상세 검증이 반복 실패하면 매핑을 `needs_recheck`로 내리고 stale 응답을 유지한 채 제한된 빈도로만 재해상도합니다.

권장 시작값:

```yaml
coupang:
  collect_interval: 15m
  hot_interval: 1h
  warm_interval: 6h
  cold_interval: 24h
  freshness: 1h
  read_refresh_timeout: 2s
  refresh_budget_per_hour: 120
  registration_budget_per_hour: 30
  resolution_budget_per_hour: 60
  registration_latency_budget: 2s
  tier_window: 24h
  hot_threshold: 3
  warm_threshold: 1
  candidate_fanout: 3
  mapping_recheck_backoff: 6h
  allow_auxiliary_fallback: true
  lookup_coalescing_enabled: true
  registration_join_wait: 2s
  read_refresh_join_wait: 2s
iris:
  room_worker_enabled: true
  room_worker_count: 8
```

### 쿠팡 동시요청 Canary 체크리스트

1. canary 1개 방만 대상으로 `coupang.lookup_coalescing_enabled=true`, `iris.room_worker_enabled=true` 적용
2. 관리자 `Feature Ops`에서 `partial_ratio`, `join_timeout_ratio`, `deferred_count`, `budget_exceeded_count`를 30분 단위로 확인
3. 동일 기간 `p95 응답시간`, `commands_10m`, `latency_samples_10m`, `reply_failed` 추이를 baseline(직전 24h)과 비교
4. `chart_skip_reasons` 상위 reason이 `insufficient_points`/`flat_without_reference_delta` 외로 급증하는지 확인
5. canary 이상 징후 없으면 방 범위를 점진적으로 확대

### 쿠팡 동시요청 롤백 절차

1. 즉시 `coupang.lookup_coalescing_enabled=false` 적용 후 재배포
2. 문제가 transport 병렬화로 의심되면 `iris.room_worker_enabled=false`도 함께 적용
3. 롤백 후 15분 동안 `partial_ratio`, `join_timeout_ratio`, `reply_failed`, `p95` 재확인
4. 지표가 baseline으로 복귀하면 원인 수정 전까지 플래그 비활성 상태 유지
5. 재활성화는 canary 1개 방부터 동일 체크리스트로 재진행

### 방별 ACL

기본 정책은 `deny`이며, 각 방은 Iris `chat_id`로 식별합니다. 표시용 채팅방 이름(`room`)이 아니라 `chat_id`를 화이트리스트 키로 사용해야 합니다.

채팅 기반 ACL 운영을 켜려면 `access.runtime_db_path`와 bootstrap super admin 정보를 같이 설정해야 합니다. 이 기능이 활성화되면 `access.rooms`는 초기 시드 데이터로 사용되고, 이후 변경은 런타임 ACL SQLite 저장소에 반영됩니다.

runtime ACL 저장소가 이미 존재하더라도, 설정에 선언된 bootstrap admin room/user principal은 startup 시 런타임 저장소에 다시 보장됩니다. 반대로 기존 runtime admin principal은 자동 삭제되지 않으므로, drift 복구 뒤에는 채팅 명령으로 stale principal을 정리해야 합니다.

예시:

```yaml
access:
  default_policy: deny
  runtime_db_path: data/access.db
  bootstrap_admin_room_chat_id: "1234567890"
  bootstrap_admin_user_id: "42"
  rooms:
    - chat_id: "1234567890"
      alias: "코인방"
      allow_intents:
        - help
        - coin
    - chat_id: "9876543210"
      alias: "장보기"
      allow_intents:
        - help
        - coupang
```

채팅 기반 ACL 운영 명령 예시:

- `관리 현황`
- `관리 상태`
- `관리 등록 [별칭]`
- `관리 코인 켜기`
- `관리 코인 끄기`
- `관리 코인방 날씨 켜기`
- `관리 관리자 목록`
- `관리 관리자 사용자 추가 <user_id>`

운영 메모:

- 관리자 명령은 `관리자 방(chat_id)`와 `관리자 사용자(user_id)`가 모두 일치해야 실행됩니다.
- 관리자 principal(관리자 방, 관리자 사용자) 변경은 bootstrap super admin만 할 수 있습니다.
- 모든 ACL 쓰기 명령은 런타임 저장소와 감사 로그에 기록됩니다.
- `access.db`에 stale admin principal이 남아 있어도 bootstrap super admin은 재시작 후 복구 경로를 유지합니다.

현재 기본 메시지 문법:

- `/도움`
- `비트코인`
- `삼성전자`
- `쿠팡 https://link.coupang.com/a/...`

### 관리자 대시보드

운영 메트릭과 방별 분석을 보려면 관리자 기능을 켠다.

```yaml
admin:
  metrics_enabled: true
  metrics_db_path: data/admin-metrics.db
  pseudonym_secret: "long-random-secret"
  enabled: true
  listen_addr: 0.0.0.0:9090
  base_path: /admin
  auth_email_header: X-Auth-Request-Email
  allowed_emails: owner@gmail.com,operator@gmail.com
  audience_scopes: owner@gmail.com|operator|*|*|*,partner@example.com|partner|tenant-a|room-hash-a|coin;stock,customer@example.com|customer|tenant-a|room-hash-a|coin
  trusted_proxy_cidrs: 127.0.0.1/32,::1/128,172.18.0.0/16
```

운영 기준:

- 메트릭 저장소에는 원문 `chat_id`, `user_id`, 메시지 본문을 저장하지 않는다.
- `chat_id`, `user_id`는 HMAC 기반 stable pseudonym으로 저장한다.
- 초기 릴리스의 관리자 surface는 읽기 전용이다.
- 외부 노출은 앱 내부 OAuth 대신 reverse proxy + Google OIDC auth proxy 구성을 권장한다.
- 권장 공개 URL은 `https://masterbot-admin.<domain>/admin/` 같은 전용 서브도메인이다.
- `audience_scopes` 형식은 `email|role|tenants(;)|rooms(;)|features(;)` 이며 `*`는 전체 허용을 뜻한다.

예시 배포 자산은 [admin-auth compose](/Users/munawiki/Workspace/jucobot/jucobot-v2.worktree/worktree-2/deployments/compose/admin-auth.yml) 와 [nginx template](/Users/munawiki/Workspace/jucobot/jucobot-v2.worktree/worktree-2/deployments/admin/nginx-admin.conf.template) 에 있다.

## 배포

- `deployments/Dockerfile`: scratch 기반 런타임 이미지(`jucobot`, `alertd` 포함)
- `deployments/compose/redroid.yml`: Redroid 전용 단계
- `deployments/compose/jucobot.yml`: JucoBot 전용 단계
- `docs/remote-deployment.md`: 원격 staged deployment, ADB/scrcpy, KakaoTalk/Iris 수동 초기화, 롤백 절차
- `docs/coupang-concurrency-rollout.md`: 쿠팡 동시요청 coalescing/room-worker canary 체크리스트 및 롤백 절차

운영 배포는 아래처럼 단계형으로 진행합니다.

```bash
make deploy-remote DEPLOY_TARGET=user@host
make remote-bootstrap DEPLOY_TARGET=user@host
make remote-up-redroid DEPLOY_TARGET=user@host
# 운영자 수동 단계: adb + scrcpy + KakaoTalk 로그인 + Iris 실행
make remote-up-jucobot DEPLOY_TARGET=user@host
```

배포/검증 규칙:

- `make deploy-remote`는 기본적으로 로컬 `./scripts/verify/local-verify.sh`를 먼저 실행합니다.
- verify를 건너뛰려면 `SKIP_LOCAL_VERIFY=1 make deploy-remote ...`를 사용합니다.
- `make remote-up-jucobot`는 배포 후 smoke 검증을 수행하고, 실패하면 기본값으로 이전 릴리스로 자동 롤백합니다.
- 자동 롤백을 끄려면 `AUTO_ROLLBACK_ON_FAILURE=0 make remote-up-jucobot ...`를 사용합니다.

로컬 회귀 차단:

- `make install-hooks`를 한 번 실행하면 pre-push 훅이 설치됩니다.
- pre-push 훅은 `go test -count=1`, `go test -race`, `go test -shuffle=on`, coverage 하한(기본 35%) 검증을 강제합니다.

## Sonar 품질 게이트

`main` 기준으로 커버리지 포함 Sonar 분석을 실행하려면 아래를 사용합니다.

```bash
export SONAR_TOKEN=<your-token>
make sonar-main
```

품질 목표 검증(기본: open issues 0, critical 0, duplication <= 1.0, total coverage >= 75, 핵심 패키지 >= 70)은 아래를 사용합니다.

```bash
export SONAR_TOKEN=<your-token>
make quality-gate
```

필요 시 임계값은 환경변수(`MIN_TOTAL_COVERAGE`, `MIN_CORE_PACKAGE_COVERAGE`, `MAX_DUPLICATION_DENSITY`)로 조정할 수 있습니다.

기본 shared 설정 템플릿은 `deployments/shared`에 있습니다. 서버 전용 값은 `/opt/jucobot/shared`에 유지되므로, 새 릴리스를 올려도 설정과 Redroid 데이터 볼륨은 재사용됩니다.
