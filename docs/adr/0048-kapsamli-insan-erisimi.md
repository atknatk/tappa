# ADR 0048 — Kapsamlı insan erişimi: `tappa_readonly` / `tappa_readwrite` ve `tappa-viewer`

- **Durum:** Kabul edildi ve **üretimde uygulandı** (2026-11-17).
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
**yalnız tappa'ya** dar bir pencere istedi. Küme paylaşımlıdır (~38 namespace, başka ürünlerin
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
| tablolar | her public tabloda SELECT (`tags`: sütun listesi, §3) | SELECT, INSERT her yerde; UPDATE, DELETE **append-only olmayan** her yerde (§3) |
| sequence | SELECT | USAGE, SELECT |
| şema | USAGE; CREATE **yok** | aynı |
| fonksiyon | SECURITY DEFINER'a EXECUTE **yok** (dosyanın son koşulu reddeder) | aynı |
| veritabanı | yalnız `tappa` (`postgres` ve `template1`'den PUBLIC'in CONNECT'i alındı) | aynı |

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

## 3. §4.3 / §4.4 / §4.7 bu rollere de uygulanır — kullanıcı *"her şeye"* dedi, ama

- **Append-only tablolar (§4.3):** `tappa_readwrite` yalnız SELECT, INSERT. Liste **yazılmadı,
  türetildi**: `public.tappa_forbid_mutation`'a bağlı satır tetikleyicisi taşıyan her tablo
  (`TestAppendOnlyTablesAreNamedByBothScripts`'in `db/migrations`'tan yaptığı türetmenin aynısı).
  Üretimde goose 107'de on iki tablo: `audit_log, billing_periods, legal_documents,
  operator_audit_log, policy_versions, punch_claim_decisions, punch_claims, punch_events,
  site_punches, tenant_plan_history, transaction_reviews, transactions` — `pg-restore-verify.sh`'in
  `trunc_tables`'ı ve `redline-check.sh`'in `APPEND_ONLY`'si ile birebir.
- **Gelecek tablolar:** varsayılan yetki (`FOR ROLE tappa_owner` — her public tablonun sahibi,
  ölçüldü) yeni tabloya `tappa_readonly`'ye SELECT, `tappa_readwrite`'a **yalnız SELECT, INSERT**
  verir. UPDATE/DELETE varsayılanda **yok**, çünkü varsayılan `CREATE TABLE` anında uygulanır, yani
  migration'ın `CREATE TRIGGER ... tappa_forbid_mutation`'ından **önce**: geniş bir varsayılan, bir
  sonraki append-only tabloya doğduğu anda UPDATE/DELETE verirdi. Yeni değişebilir bir tabloda
  UPDATE/DELETE, dosya yeniden koşunca gelir; o zamana kadar `permission denied` (kapalı yön,
  gürültülü). İki kemer: varsayılan satırının ardından açık bir `REVOKE UPDATE, DELETE ...` ve
  append-only tablolar için ayrı bir REVOKE döngüsü — test her birinin tek başına çıkarılmasının
  ötekiyle maskelendiğini, ikisi birden çıkarılınca kırmızı olduğunu ölçer.
- **`tags` (§4.4, §4.7, kullanıcı kuralı "plaket satırı silinmez"):** tablo düzeyi yetki yok; sütun
  yetkileri `tappa_app`'ın **kendi** `tags` sütun yetkilerinden türetilir (ikisi ayrışamaz), eksi:
  `aes_key_ref`, `app_key_ref` (sarmalanmış anahtarlar — `tappa_app` da okuyamaz), ve UPDATE'ten
  `last_ctr`: geri sarmayı tetikleyici reddeder ama **ileri sıçramayı** hiçbir şey reddetmez — tek bir
  büyük değer sonraki her gerçek dokunuşu replay yapar ve sayaç geri gelemez. INSERT ve DELETE yok
  (DELETE + INSERT `last_ctr`'ı 0'a sıfırlar — `pg-restore-verify.sh` başlığında ölçülü).
- **`goose_db_version`:** yalnız SELECT (deploy'un defteri, veri değil).
- **Kalan her şey:** `tappa_readwrite` BYPASSRLS ile her tenant'ın satırını değiştirir. Tetikleyiciler
  onlar için de ateşlenir (süper kullanıcı değiller, `session_replication_role`'ü değiştiremezler).

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

## 5. Sınırlar — sayılı, gizlenmiyor

1. **Veritabanı adları gizlenemez.** `pg_database` her role okunur; `\l` dört adı listeler. Bu
   instance'ta başka ürünün veritabanı yok, yani gizlenecek bir ad yok — ama bu bir yapı gerçeği
   değil bir **durum** gerçeğidir: buraya ikinci bir veritabanı konursa adı görünür. Aynı şekilde
   `pg_roles` tappa'nın on rol adını gösterir.
2. **`tappa_readwrite` güçlüdür.** Her tenant'ın satırını değiştirir ve siler (FK'ler RESTRICT /
   NO ACTION; ölçüldü: cascade eden FK yok, yani silme append-only satırlara ya da `tags`'e
   ulaşamaz), `transactions`'a ve `audit_log`'a **INSERT** edebilir (sahte mesai satırı, sahte iz —
   §4.3 düzeltmenin yeni kayıt olduğunu söyler, bu yüzden INSERT açık), parola özetlerini ve
   sarmalanmış TOTP sırlarını okuyup değiştirebilir. Kullanıcının *"her şeye"*si budur.
3. **`tappa_readonly` da her şeyi okur**: işçi verisi, e-posta adresleri, parola özetleri, token
   özetleri. Bu bir GDPR erişimidir; kimin elinde olduğu kayıt altındadır (bu ADR).
4. **Tünel sızıntısı:** port-forward açıkken yerel makinenin `localhost:15432`'si o makinedeki her
   sürece açıktır (yine de parola ister).
5. **Yeni tablolar** dosya yeniden koşana kadar `tappa_readwrite` için UPDATE/DELETE'siz kalır (kapalı
   yön). Yeni bir SECURITY DEFINER fonksiyonu PUBLIC'ten REVOKE edilmezse bu roller onu çalıştırabilir
   (uygulama da) — dosyanın son koşulu bir sonraki koşuda bunu reddeder; arada sınır migration'ların
   `REVOKE ... FROM PUBLIC` disiplinidir.
6. **İptal ~10 s gecikmelidir** (§4).
7. **`tappa-db-access` Secret'ı** (iki parola) `ns/tappa`'dadır; `github-deployer` onu `get`
   edemez (yalnız iki adlı Secret), ama 01-rbac.yaml'ın sayılı sınırı (Job ile her Secret okunabilir)
   onu da kapsar.

## 6. Geri alma

`kubectl -n tappa delete secret tappa-viewer-token` (küme erişimi, ~10 s içinde) ·
`ALTER ROLE tappa_readonly NOLOGIN; ALTER ROLE tappa_readwrite NOLOGIN;` (veritabanı) — ayrıntı ve
parola döndürme: `deploy/README.md` → *"Kapsamlı erişim (ADR 0048)"*. Rolleri **DROP etmek**
önerilmez: yetkileri taşıyan bir yedek, rol yoksa B YOLU'nda yüklenemez (§2.1).
