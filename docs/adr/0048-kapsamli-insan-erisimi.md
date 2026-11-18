# ADR 0048 — Kapsamlı insan erişimi: `tappa_readonly` / `tappa_readwrite` ve `tappa-viewer`

- **Durum:** Kabul edildi ve **üretimde uygulandı** (2026-11-17). **Revizyon 2 (2026-11-18, güvenlik denetiminin 2. turu):** `id`/`tenant_id`
  UPDATE'i ve RESTRICTIVE politikanın daralttığı fiiller çıkarıldı (§5 madde 2); iz bırakmama sınırı (§5 madde
  12); bağlantı log'u PG17'de rol düzeyinde kurulamıyor (ölçüldü). **Revizyon 1 (2026-11-17, iki denetimin
  bulguları):** `tappa_readwrite`'ın yazma yetkisi artık `tappa_app`'ınkinin kopyası (§3); iptal açık
  oturumları da keser (§6); §5'e dört sınır eklendi.
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-17 — istek: *"Tappa uygulamasına readonly ve
  readwrite database kullanıcısı oluştur. Ek olarak kubectl'de tappa erişimi olsun ama başka
  namespace/database'e erişemesin, database adını dahi okuyamasın, namespace'lerin adını dahi
  göremesin; sadece tappa yetkili olacak."* İki ek karar: **(K-1)** *"database olarak tappa'da her
  şeye erişebilsin ama o instance'da başka db varsa adını dahi göremesin"* → iki rol de **tüm
  tenant'ları** görür (BYPASSRLS); **(K-2)** kubectl yetkisi *"görüntüle + log + port-forward"*.
  **(K-3)** kimlik: ServiceAccount + **iptal edilebilir** token Secret'ı.
- **Değiştirdiği kural:** CLAUDE.md §4.5 — dördüncü bilinçli istisna (bir insanın doğrudan erişimi).
  §4.3, §4.4, §4.7 **değişmez** ve bu ADR onları bu rollere de uygular (aşağıda §3).
- **Dosyalar:** `scripts/db-init/01-roles.sql` (SCOPED ACCESS ROLES bloğu) ·
  `deploy/db/scoped-access.sql` · `deploy/k8s/02-viewer-rbac.yaml` ·
  `deploy/k8s/viewer/tappa-viewer-token.yaml` · `scripts/verify-deployment.sh` (db-role) ·
  `internal/db/scopedaccess_test.go` · `internal/db/privcatalog_test.go` · `deploy/README.md` →
  *"Kapsamlı erişim (ADR 0048)"*.

## 1. Bağlam

Şimdiye kadar üretim veritabanına bir insan yalnız iki yoldan ulaşıyordu: cluster-admin
kubeconfig'iyle `kubectl exec statefulset/tappa-postgres -- psql -U tappa_owner` (süper kullanıcı,
RLS yok, DDL var) ya da hiç. Kullanıcı, bu süper kullanıcıyı ve cluster-admin'i vermeden,
**yalnız tappa'ya** dar bir pencere istedi. Küme paylaşımlıdır (40 namespace — ölçüldü, başka ürünlerin
veritabanları dahil); Postgres instance'ı ise tappa'nındır (`statefulset/tappa-postgres`, PG 17.10;
veritabanları `postgres`, `tappa`, `template0`, `template1` — başka uygulama veritabanı **yok**,
ölçüldü 2026-11-17).

## 2. Karar

### 2.1 Veritabanı — iki rol

| | `tappa_readonly` | `tappa_readwrite` |
|---|---|---|
| nitelik | NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION **BYPASSRLS**, LOGIN (parola bant dışı) | aynı |
| üyelik / sahiplik | yok / yok (dosyanın ön koşulu reddeder) | aynı |
| oturum | `default_transaction_read_only = on` (kemer) | — |
| tablolar | her public tabloda SELECT (`tags`: sütun listesi, §3) | aynı SELECT + **`tappa_app`'ın kendi INSERT/UPDATE/DELETE'i**, aynı düzeyde (tablo ya da sütun) — eksi `tags` istisnaları ve `goose_db_version` (§3), eksi `id`/`tenant_id` UPDATE'i (UPDATE hep sütun listesi) ve RESTRICTIVE politikanın daralttığı fiiller (§5 madde 2) |
| sequence | SELECT | SELECT; USAGE yalnız `tappa_app`'ın USAGE'ı olduğu yerde (bugün hiçbiri) |
| şema | USAGE; CREATE **yok** | aynı |
| fonksiyon | SECURITY DEFINER'a EXECUTE **yok** (dosyanın son koşulu reddeder) | aynı |
| veritabanı | yalnız `tappa` (`postgres` ve `template1`'den PUBLIC'in CONNECT'i alındı) | aynı |
| `TEMPORARY` (`tappa`) | **var**, PUBLIC'ten — oturuma özel geçici tablo; alınmadı, belgelendi (§5) | aynı |
| varsayılan yetki (yeni tablo) | SELECT | **yalnız SELECT** — yazma, dosyanın bir sonraki koşusunda `tappa_app`'tan kopyalanır |

**Neden migration değil:** migration'lar geliştirme, CI ve sandbox'ta da koşar; bir BYPASSRLS rolüne
her tabloyu veren bir migration tam olarak redline R5b'nin durdurduğu şeydir. **Neden init betiği
değil:** `/docker-entrypoint-initdb.d` yalnız boş PGDATA'da bir kez koşar; üretim hacmi 93 günlüktür
(ölçüldü). Dolayısıyla `01-rbac.yaml`'ın veritabanı karşılığı: operatörün, idempotent, tek
transaction'da (`psql -1 -f -`) koşan dosyası `deploy/db/scoped-access.sql`.

**Neden rollerin kendisi `01-roles.sql`'de (geliştirmede de):** üretim bu rollere GRANT verdiği anda
her `pg_dump` o GRANT'ları taşır. B YOLU (taze pod) dökümü `ON_ERROR_STOP=1` ile yükler; rol yoksa
ilk `GRANT ... TO tappa_readonly`'de durur — ve pg_dump ACL'leri **en sona** yazar (ölçüldü: yerel
dökümde ilk böyle satır 11 770'te, son `COPY` ~8 900'de), yani bütün veri yüklendikten sonra.
`pg-restore-verify.sh`'in rol envanteri (`pg-backup.sh` ile aynı sorgu) bu iki rolü `bypassrls=t`
ile ister. Rol yalnız buradan doğabilir; diğer bütün roller de böyle (OPERATOR/HOLDING/FEED/IDENTITY
blokları). Geliştirmede iki rol **NOLOGIN, parolasız, tablo yetkisiz**dir (`privCatalogAllowed`
bunu satır satır tutar: yalnız USAGE ve PUBLIC'in CONNECT/TEMPORARY'si).

### 2.2 Küme — `tappa-viewer`

`ServiceAccount` + `Role` + `RoleBinding`, üçü de `ns/tappa`'da; **küme kapsamlı hiçbir nesne yok**.
Fiiller: `pods, services, endpoints, configmaps, events, persistentvolumeclaims`, `deployments,
statefulsets, replicasets`, `jobs, cronjobs`, `ingresses, networkpolicies`, `events.k8s.io/events`
üzerinde `get/list/watch`; `pods/log` `get`; `pods/portforward` `get, create` **yalnız
`tappa-postgres-0`**. Secret yok, exec/attach yok, hiçbir yazma fiili yok — pod üretebilen bir kimlik
namespace'in her Secret'ını bağlayıp okuyabilir (RBAC'sız, kubelet çözer — `01-rbac.yaml`'da
ölçülü) ve küme ağından başka ürünlerin veritabanlarına ulaşabilir.

Token bir `kubernetes.io/service-account-token` Secret'ıdır (`tappa-viewer-token`): silinince ölür.
TokenRequest token'ı (`kubectl create token`) süresi dolana dek geri alınamaz — bu yüzden seçilmedi.
Kubeconfig'in sunucusu `https://10.43.0.1:443`'tür (tailnet subnet router); **ts.net proxy değil**,
çünkü o bearer token'ı yok sayıp Tailscale kimliğini taklit eder (`deploy/README.md` adım 5).

## 3. Yazma modeli: `tappa_readwrite` = "`tappa_app`'ın yazabildiği her şey, RLS'siz"

**Revizyon 1'in kararı (ana oturum, öneri seçildi).** İlk sürüm UPDATE/DELETE'i *"append-only tetikleyicisi
olmayan her tabloya"* veriyordu ve INSERT'i her tabloya — bu bir **yasak listesiydi**, yani açık yönde
hata veriyordu. Güvenlik denetimi ölçtü:

- **SARI-1:** `feed_counters` (yalnız `tappa_feedwriter` ve `tappa_holdingdefiner` yazar) açıktı. `last_seq`
  geri sarılırsa `punch_feed()` `punch_events`'in birincil anahtarında 23505 alır ve o tenant'ın her `ok`
  dokunuşu **kayıt yazılmadan** düşer — §4.6.
- **SARI-2:** `holding_members`, `holdings`, `platform_admins`, `platform_sessions`, `oauth_clients`,
  `operator_read_tickets`, `admin_identities` yalnız definer'larca yazılır ama açıktı: kiracılar arası
  eş üyelik, izsiz OAuth istemcisi, operatör enrollment'ının ele geçirilmesi.

**Yeni kaynak bir izin listesidir:** `tappa_app`'ın **kendi** tablo düzeyi (`pg_class.relacl`) ve sütun
düzeyi (`pg_attribute.attacl`) INSERT/UPDATE/DELETE'i, dosyanın koştuğu anda kopyalanır. O yetkiler her
migration'da matrisle denetleniyor (`privcatalog_test.go`, `m11schema_test.go`), türetme bu denetimi
miras alır ve **kapalı yönde** hata verir: `tappa_app`'ın yazamadığını bu rol de yazamaz. Ölçüldü (üretim,
goose 107, yeniden uygulamadan sonra): 47 tablonun her birinde readwrite'ın INSERT/UPDATE/DELETE'i
`tappa_app`'ınkiyle **aynı** — tek fark `tags` (aşağıda); sekiz definer tablosunun hepsinde yazma 0.

**Dosyanın son koşulları** (biri düşerse transaction geri alınır): (1) readwrite'ın her yazma yetkisi
sütun sütun `tappa_app`'ta da var; (2) yukarıdaki sekiz tabloda yazma 0 — `tappa_app`'a günün birinde
oralarda bir yazma verilse bile (test bunu `GRANT UPDATE ON feed_counters TO tappa_app` ile ölçer: dosya
reddeder); (3) `tappa_forbid_mutation` satır tetikleyicisi taşıyan her tabloda UPDATE/DELETE 0; (4)
`tags` istisnaları; (5) readonly hiçbir şey yazamaz; (6) hiçbir SECURITY DEFINER'a EXECUTE yok.

- **Append-only tablolar (§4.3):** bu modelde kendiliğinden kapalı — `tappa_app` hiçbirinde UPDATE ya da
  DELETE taşımıyor. Üretimde goose 107'de on iki tablo: `audit_log, billing_periods, legal_documents,
  operator_audit_log, policy_versions, punch_claim_decisions, punch_claims, punch_events, site_punches,
  tenant_plan_history, transaction_reviews, transactions` — `pg-restore-verify.sh`'in `trunc_tables`'ı ve
  `redline-check.sh`'in `APPEND_ONLY`'si ile birebir. INSERT yalnız `tappa_app`'ın INSERT'i olan yerde
  (`transactions`, `audit_log`, `punch_claims` … — çoğu **sütun düzeyinde**); `punch_events`,
  `site_punches`, `operator_audit_log`, `tenant_plan_history`, `legal_documents`'e artık INSERT de yok.
- **`tags` (§4.4, §4.7, kullanıcı kuralı "plaket satırı silinmez") — `tappa_app`'tan açık çıkarmalar:**
  tablo düzeyi yetki yok; INSERT yok (`tappa_app`'ta var); DELETE yok; `aes_key_ref`, `app_key_ref` okunmaz
  ve yazılmaz; **`last_ctr` UPDATE yok — `tappa_app`'ta VAR** (atomik sayaç ilerletmesi): geri sarmayı
  tetikleyici reddeder ama ileri sıçramayı hiçbir şey reddetmez — tek bir büyük değer sonraki her gerçek
  dokunuşu replay yapar ve sayaç geri gelemez.
- **`goose_db_version`:** yalnız SELECT (`tappa_app`'ta da yazma yok).
- **Gelecek tablolar:** varsayılan yetki (`FOR ROLE tappa_owner` — her public tablonun sahibi, ölçüldü)
  yeni tabloya iki role de **yalnız SELECT** verir. Varsayılan, `CREATE TABLE` anında uygulanır, yani
  migration'ın birkaç satır sonraki GRANT/REVOKE'larından ve append-only tetikleyicisinden önce; doğru
  kaynak yalnız kopyadır. Yeni bir tabloda yazma, dosya yeniden koşunca gelir (kapalı yön, gürültülü).
- **Kalan her şey:** readwrite, `tappa_app`'ın değiştirebildiği her satırı **her tenant'ta** değiştirir.
  Tetikleyiciler onlar için de ateşlenir (süper kullanıcı değiller, `session_replication_role`'ü
  değiştiremezler).

## 4. Ölçümler (2026-11-17, üretim)

- **Kimlik, kendi token'ıyla, `10.43.0.1` üzerinden:** `auth whoami` →
  `system:serviceaccount:tappa:tappa-viewer`. `get ns`, `get pods -A`, `get pods -n kube-system`,
  `get secrets`, `get secret tappa-secrets`, `get secret tappa-viewer-token` → Forbidden. `exec
  tappa-postgres-0`, `exec deploy/tappa` → *"cannot create resource pods/exec"*. `run`, `create job`,
  `delete pod` → Forbidden. `get pods,svc,deploy,sts,ingress,jobs,cronjobs,cm,pvc,networkpolicy,events`,
  `logs deploy/tappa`, `describe pod` → exit 0. `auth can-i --list`: yalnız yukarıdaki kurallar +
  self-review + keşif/`/version`/`/healthz` gibi non-resource URL'ler; **hiçbir namespace adı yok**.
  Kümenin hiçbir RoleBinding'i ve varsayılanlar dışındaki hiçbir ClusterRoleBinding'i bu SA'nın
  gruplarını (`system:authenticated`, `system:serviceaccounts`, `system:serviceaccounts:tappa`) anmıyor.
- **Port-forward fiilleri (`-v=8`):** `[create]` → WebSocket reddi, SPDY geri düşüşüyle tünel;
  `[get]` → tünel yok; `[get, create]` → WebSocket 101, geri düşüş yok. Uygulama pod'una port-forward
  → reddedildi. Sunucu tünelden gelen oturumu `127.0.0.1`'den görür.
- **İptal (geçici ikinci SA, aynı görevde yaratılıp silindi):** Secret silindikten sonra 1 s ve 5 s'de
  token hâlâ geçerliydi, 10 s'den itibaren `Unauthorized` — API sunucusunun token önbelleği ~10 s.
- **Veritabanı, tünel üzerinden:** iki rol de 5 tenant'ı görür (GUC yokken; `transactions`'ta 2 tenant,
  189 satır). `tappa_readonly`: INSERT → *"read-only transaction"*; `BEGIN READ WRITE` + INSERT/UPDATE →
  *"permission denied"*; `tags.aes_key_ref` → reddedildi; `op_touch_session` → reddedildi.
  `tappa_readwrite`: `BEGIN; UPDATE locations ... ; ROLLBACK` → `UPDATE 1`; `transactions` UPDATE ve
  DELETE, `audit_log` UPDATE → **42501 permission denied** (tetikleyicinin P0001'i değil — yetki yok);
  `tags` `last_ctr`/`aes_key_ref` UPDATE ve DELETE → reddedildi; `goose_db_version` UPDATE →
  reddedildi; `CREATE TABLE` → *"permission denied for schema public"*. İkisi de `postgres`/`template1`'e
  → *"User does not have CONNECT privilege"*; `template0` bağlantı kabul etmiyor.
- **db-role kapısı:** bir `tappa_readonly` oturumu açıkken eski sorgu `tappa_readonly from 127.0.0.1`
  diye bir deploy'u düşürecekti; yeni sorgu boş döndü (uygulama `10.42.0.161`'den).

- **Revizyon 1, yeniden uygulamadan sonra (tünel üzerinden, `BEGIN … ROLLBACK`, `WHERE false`):**
  readwrite'ın `feed_counters`, `holding_members`, `oauth_clients`, `platform_admins` üzerinde
  `has_table_privilege` INSERT/UPDATE/DELETE → hepsi `f`; gerçek denemeler (`UPDATE feed_counters`,
  `INSERT INTO holding_members`, `INSERT INTO oauth_clients`, `UPDATE platform_admins`) → *"permission
  denied"*; `UPDATE locations …` → hâlâ `UPDATE 1`; `transactions` UPDATE → *"permission denied"*.
  readonly değişmedi: `ro=on`, 5 tenant, `BEGIN READ WRITE` + UPDATE → *"permission denied"*, 47
  tablonun 46'sında tablo düzeyi SELECT (+ `tags` sütun listesi). Varsayılan yetki:
  `tappa_readwrite=r` (tablolar ve sequence'lar).
- **Açık tünel ve iptal (geçici ikinci SA, `tappa-viewer` Role'üne bağlı, aynı görevde silindi):** tünel
  açıkken bir psql oturumu başladı; token Secret'ı silindi; 15 s sonra **aynı tünelden yeni bir psql
  bağlantısı da açıldı** (`ok`), yeni bir port-forward ise *"the server has asked for the client to
  provide credentials"* ile düştü; ilk oturum 25 s'lik sorgusunu bitirip yaşamaya devam etti. Yani
  token'ı silmek **kurulmuş tüneli kesmez** — §6.
- **Keşif:** viewer token'ıyla `kubectl api-versions` 50'yi aşkın API grubunu listeler; üçü
  (`cert-manager.io`/`acme.cert-manager.io`, `keda.sh`/`eventing.keda.sh`, `tailscale.com`) küme
  namespace'leri `cert-manager`, `keda`, `tailscale` ile aynı adı taşır (ölçüldü, 40 namespace) — §5.

## 5. Sınırlar — sayılı, gizlenmiyor

1. **Veritabanı adları gizlenemez.** `pg_database` her role okunur; `\l` dört adı listeler. Bu
   instance'ta başka ürünün veritabanı yok, yani gizlenecek bir ad yok — ama bu bir yapı gerçeği
   değil bir **durum** gerçeğidir: buraya ikinci bir veritabanı konursa adı görünür. Aynı şekilde
   `pg_roles` tappa'nın on rol adını gösterir.
2. **`tappa_readwrite` güçlüdür.** `tappa_app`'ın değiştirebildiği her satırı **her tenant'ta**
   değiştirir ve siler (FK'ler RESTRICT / NO ACTION; ölçüldü: cascade eden FK yok, yani silme
   append-only satırlara ya da `tags`'e ulaşamaz), `transactions`'a ve `audit_log`'a `tappa_app`'ın
   sütunlarıyla **INSERT** edebilir (sahte mesai satırı, sahte iz — §4.3 düzeltmenin yeni kayıt
   olduğunu söyler). Definer'ların tabloları (§3) kapalıdır. ⚠️ **Ama "`tappa_app` kadar" uygulama
   sınırıyla aynı şey değildir:** BYPASSRLS iki şeyi kendiliğinden ekler ve ikisi de Revizyon 2'de
   (2026-11-18) çıkarıldı — (a) RESTRICTIVE politikalar readwrite'a uygulanmaz:
   `oauth_codes_delete_expired`, `oauth_tokens_delete_expired`, `webhook_deliveries_delete_settled`
   `tappa_app`'ın DELETE'ini süresi dolmuş/yerleşmiş satırlara daraltır, readwrite her satırı
   silebilirdi → bu üç tabloda readwrite'ın DELETE'i **yok** (kural `pg_policy`'den türetilir: bir
   fiili daraltan RESTRICTIVE politika varsa fiil kopyalanmaz); (b) `employees`, `departments`,
   `locations`, `policies`, `policy_attachments`'ta `tappa_app` tablo düzeyi UPDATE taşır, yani `id` ve
   `tenant_id` UPDATE'ini de — RLS'nin `WITH CHECK`'i onu tenant içinde tutar, readwrite'ta ise
   başvurusu olmayan bir satır başka tenant'a taşınabilirdi → readwrite'ın UPDATE'i **her tabloda
   sütun listesidir ve `id`, `tenant_id`'yi içermez**. İkisi de "readwrite ⊆ `tappa_app`" ilkesinin
   içinde (daha dar) ve son koşul (7) ile (8) ölçer. Kalan fark: readwrite `tappa_app`'ın
   değiştirebildiği satırı **her tenant'ta** değiştirir.
3. **`tappa_readonly` da her şeyi okur**: işçi verisi, e-posta adresleri, parola özetleri, token
   özetleri. Bu bir GDPR erişimidir; kimin elinde olduğu kayıt altındadır (bu ADR).
4. **Tünel sızıntısı:** port-forward açıkken yerel makinenin `localhost:15432`'si o makinedeki her
   sürece açıktır (yine de parola ister).
5. **Yeni tablolar** dosya yeniden koşana kadar `tappa_readwrite` için yalnız SELECT'tir — yazma yok
   (kapalı yön). Yeni bir SECURITY DEFINER fonksiyonu PUBLIC'ten REVOKE edilmezse bu roller onu çalıştırabilir
   (uygulama da) — dosyanın son koşulu bir sonraki koşuda bunu reddeder; arada sınır migration'ların
   `REVOKE ... FROM PUBLIC` disiplinidir.
6. **İptal ~10 s gecikmelidir ve kurulmuş tüneli kesmez** (§4) — açık oturumları NOLOGIN +
   `pg_terminate_backend` keser (§6).
7. **`tappa-db-access` Secret'ı** (iki parola) `ns/tappa`'dadır; `github-deployer` onu `get`
   edemez (yalnız iki adlı Secret), ama 01-rbac.yaml'ın sayılı sınırı (Job ile her Secret okunabilir)
   onu da kapsar.
8. **API keşfi kurulu yazılımı gösterir.** `system:discovery` (kümenin varsayılanı, her kimliğe bağlı)
   `kubectl api-resources` / `api-versions`'ı açar; CRD grupları kurulu operatörleri anlatır ve üçü
   (`cert-manager`, `keda`, `tailscale`) namespace adlarıyla çakışır. Bir Role bunu kapatamaz;
   kapatmak kümenin varsayılan ClusterRoleBinding'ini değiştirmek olurdu (bütün kümeyi etkiler, bu
   işin kapsamı değil). Namespace **listesi** değildir; `get ns` yine Forbidden.
9. **Tailnet rotası RBAC'ın dışındadır.** Subnet router `10.43.0.0/16`'nın (Service CIDR) tamamını
   duyurur. Alıcının Tailscale ACL'i yalnız `10.43.0.1:443`'e izin vermezse, `kube-dns`'e PTR taraması
   bütün `svc.namespace` adlarını ve ClusterIP'ler başka ürünlerin servislerini açar — kubeconfig'siz,
   RBAC'sız. Kapatan tek şey ACL'dir: alıcının grant'ı **`10.43.0.1:443` ile sınırlanmalı**
   (kullanıcının işi; örnek parça `deploy/README.md` → *"Kapsamlı erişim"* 0. adım). Bu ADR anında
   alıcının ACL'i **ölçülmedi**.
10. **Viewer Postgres pod'unun log'unu okur** (`pods/log`), ve Postgres başarısız bir ifadeyi
   `log_min_error_statement = error` ile `STATEMENT:` satırına yazar. Düz `ALTER ROLE … PASSWORD '…'`
   başarısız olursa parola **düz metin** o log'a düşer — viewer'ın okuyabildiği yere. Parola yalnız
   psql'in `\password`'ü ile değiştirilir (istemcide SCRAM'a çevrilir; runbook 3. adım).
11. **`TEMPORARY` açık** (PUBLIC'ten, `tappa` veritabanında): iki rol de oturuma özel geçici tablo
   yaratabilir (readonly'nin varsayılan READ ONLY'si bunu engeller, `BEGIN READ WRITE` engellemez).
   Başka oturum onları görmez; kalıcı veri değildir. Alınmadı, çünkü `REVOKE TEMPORARY … FROM PUBLIC`
   `tappa_app` ve `tappa_operator`'ın yetkisini de değiştirir; ölçüldü: hiçbir uygulama yolu geçici tablo
   yaratmıyor (ağaçtaki iki geçici tablo, `cmd/opadmin` ve `cmd/rotatekek`, `tappa_owner` olarak koşar) —
   yani alınabilirdi, ama bedeli üretim uygulamasının yetkisine dokunmaktır ve kazancı küçüktür.

12. **readwrite'ın yazdıkları satır düzeyinde iz bırakmaz.** Yalnız ne yaptığı değil, **ne zaman bağlı
   kaldığı da loglanmıyor**, ve ikisi de ölçüldü:
   - *Bağlantı log'u kurulamadı.* Planlanan kemer rol düzeyinde `log_connections = on` /
     `log_disconnections = on` idi. PostgreSQL 17.10 bunu reddediyor: süper kullanıcı olarak, transaction
     içinde ve dışında `ALTER ROLE … SET log_connections = on` → *"parameter "log_connections" cannot be
     set after connection start"* (55P02). İki ayar `superuser-backend` bağlamındadır. Rolün kendisi de
     kapatamaz (aynı hata). Sunucu genelinde açmak **her** bağlantıyı loglar — `pg_isready` sondaları
     dahil — ve ayrı bir karardır; bu ADR onu vermiyor.
   - *İfade log'u bilerek yok.* `log_statement = 'mod'` literal değerleri (e-posta adresleri, bir
     `webhook_endpoints.url` — içinde sorgu dizesiyle bir sır taşıyabilen adres) Postgres log'una basardı.
     O log toplanıyor: ölçüldü (2026-11-18), SigNoz otel ajanının (`signoz/k8s-infra-otel-agent`)
     `filelog/k8s` alıcısı `/var/log/pods/*/*/*.log`'u okur ve `exclude` listesi `tappa`'yı saymaz — yani
     `tappa-postgres-0`'ın log'u ClickHouse'a gider. Bu CLAUDE.md §7'yi (e-posta adresi asla loglanmaz,
     ADR 0022 §10) ihlal ederdi. `pgaudit` kurulu değil.
   - **Sonuç:** bir `transactions` (ya da `audit_log`, `employees` …) satırının readwrite'tan mı
     uygulamadan mı geldiği **ayırt edilemez**. Elde kalan izler: canlı `pg_stat_activity`
     (`usename`, `client_addr = 127.0.0.1`, `backend_start`) — anlık, saklanmaz; ve **reddedilen**
     ifadeler: `log_min_error_statement = error` başarısız ifadeyi `ERROR` + `STATEMENT` satırıyla
     loglar (ölçüldü: readwrite'ın reddedilen `UPDATE locations SET tenant_id …` denemesi log'da), ama
     `log_line_prefix` (`%m [%p] `) rol adını taşımaz — yalnız saat ve pid.

## 6. Geri alma ve iptal

🔴 **Token'ı silmek kurulmuş tüneli kesmez** (§4: silindikten 15 s sonra aynı tünelden yeni bir psql
bağlantısı da açıldı). Port-forward akışı API sunucusundan kubelet'e bir kez yetkilendirilir; token
yalnız **yeni** tünelleri durdurur (~10 s içinde). Erişimi gerçekten kesmek üç adımdır, bu sırayla:

1. **Veritabanı girişini kapat:** `ALTER ROLE tappa_readonly NOLOGIN; ALTER ROLE tappa_readwrite NOLOGIN;`
   — açık tünelden yeni bağlantı artık giriş yapamaz.
2. **Açık oturumları kes:** `SELECT pg_terminate_backend(pid, 5000) FROM pg_stat_activity WHERE usename IN
   ('tappa_readonly', 'tappa_readwrite');` (5 sn bekler, sonra bağlı oturum sayısının 0 olduğu okunur) (README'nin operatör yüzeyi geri almasındaki desen).
3. **Küme erişimini kapat:** `kubectl -n tappa delete secret tappa-viewer-token` — yeni tünel ~10 s sonra
   açılamaz ve yeni her kubectl isteği `Unauthorized` olur. ⚠️ Kurulmuş `kubectl get -w` ve `logs -f`
   akışları da port-forward gibi istek başında yetkilendirilir ve **sürer**; onları kesen token değil,
   akışın kendi sonudur.

Ayrıntı ve komutlar: `deploy/README.md` → *"Kapsamlı erişim (ADR 0048)"* 6. adım. Rolleri **DROP etmek**
önerilmez: yetkileri taşıyan bir yedek, rol yoksa B YOLU'nda yüklenemez (§2.1).
