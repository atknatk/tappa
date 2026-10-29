# Tappa — deployment (M8-02, packaging + cluster manifests)

Bu dizin **paketleme ve küme manifestlerini** taşır. M8-02'nin ikinci yarısı
(deploy/rollback/olay müdahalesi runbook'u, KEK döndürme aracı, DPA) burada
**yoktur**; bu dosya yalnızca *bu dosyaların nasıl kullanılacağını* ve
**kabul edilmiş sınırları** yazar.

Hedef: `https://taptime.mt` · küme: k3s v1.35.4, tek node
(`k8s-1.fsn1.private`, Hetzner fsn1, `144.76.158.60`).

> **Domain cutover (2026-09-02):** birincil host artık `taptime.mt`. Eski
> `tappa.everva.com.tr` M8 boyunca deploy hedefiydi; artık yalnızca `taptime.mt`'ye
> **kalıcı 301 redirect** yapıyor (`deploy/k8s/40-ingress.yaml`, Ingress
> `tappa-redirects`, `$request_uri` korunur), böylece cutover'dan önce basılmış
> linkler ve plaketler 404 vermez. `www.taptime.mt` de aynı Ingress'ten apex'e
> 301'lenir — redirect-only, hiçbir zaman tap işlemez.

---

## Ne nereden gelir

| Dosya | Ne | Kim uygular |
|---|---|---|
| `../Dockerfile` | iki imaj: `--target app` ve `--target migrate` | `deploy.yml` |
| `k8s/00-namespace.yaml` | namespace + `pod-security enforce=restricted` | `deploy.yml` (**ilk kez** operatör) |
| `k8s/01-rbac.yaml` | 🔴 **deploy'un kendi kimliği** — SA + Role + RoleBinding + tek adlı ClusterRole/Binding | **operatör** (bir kez; deploy **kendi rolünü değiştiremez**) |
| `k8s/05-config.yaml` | sırsız ortam değişkenleri | `deploy.yml` |
| `examples/secret.example.yaml` | **şablon**, değer yok, adı **canlıyla çakışmaz** | **operatör** (kopyasını) |
| `examples/externalsecret.example.yaml` | **önerilen** yol: Infisical + external-secrets | **operatör** |
| `k8s/12-networkpolicy.yaml` | 5432'ye yalnız Tappa pod'ları | `deploy.yml` |
| `k8s/10-postgres.yaml` | StatefulSet + PVC (`local-path`) + headless Service | `deploy.yml` |
| `k8s/20-app.yaml` | Deployment + Service | `deploy.yml` |
| `k8s/30-migrate-job.yaml` | goose Job (`tappa_owner`) | `deploy.yml` |
| `k8s/40-ingress.yaml` | Ingress (`nginx`, `letsencrypt-prod`) | `deploy.yml` |
| `k8s/41-operator-ingress.yaml` | operatör host'u `ops.taptime.mt`'nin iki Ingress'i (`/` 24k, `/operator/legal` Exact 320k; K4: IP kısıtı **yok** — karar 2026-10-14) — T112 | `deploy.yml`, **yalnız yüzey yapılandırılmışken** (`tappa-secrets`'ta `TAPPA_OPERATOR_HOST` 0 bayttan büyükken; değeri okunmaz) ve rollout beklendikten **sonra** — T119 (silmek **operatör**: deploy kimliğinde `delete` yok; yüzey kapanırken — *"Geri alma — yüzeyi kapat"*) |
| `k8s/42-api-ingress.yaml` | entegrasyon API'sinin host'u `api.taptime.mt`'nin Ingress'i (yalnız `/v1`, 80k, kendi sertifikası `api-taptime-tls`) — M11 API-13a; `05-config.yaml`'ın `TAPPA_API_HOST`'u ile **aynı değişiklikte** | `deploy.yml` (rollout beklemesinden **sonra**) |
| `k8s/50-backup.yaml` | 🔴 **gecelik yedek CronJob'ı** (02:30 Malta) | **operatör** (bir kez; deploy'un `batch/cronjobs` yetkisi **yok** ve bilerek yok) |
| `k8s/55-heartbeat.yaml` | 🔴 **uptime heartbeat CronJob'ı** (her 5 dk; healthchecks.io'ya *"yaşıyor"* ya da `/fail` — T116) | **operatör** (bir kez; aynı gerekçe — adım 10) |
| `observability/signoz-alert-rules.json` | 🔴 **SigNoz'un uyarı kuralları** (M8-03 tablosunun her satırı için bir kural, ClickHouse-SQL; T118, 8. kural T122) — kanal yalnız **adıyla** (`tappa-healthchecks`), adres yok; küme manifesti **değildir** | **operatör** (SigNoz API'siyle, bir kez — adım 10 (f)) |
| `k8s/postgres-init/02-app-password.sh` | `tappa_app`'e **girişi açan** üretim script'i | ConfigMap içinde |
| `../scripts/pg-backup.sh` | dump **ve içeriğinin doğrulanması** (satır sayısı, tablo kümesi, RLS/GRANT) | ConfigMap içinde (`deploy.yml` `--from-file`) |
| `../scripts/pg-backup-ship.sh` | dump'ı **node dışına** taşır + saklama süresini uygular + sonucu healthchecks.io'ya bildirir (T116; bildirim yedeğin sonucunu **değiştirmez**) | ConfigMap içinde (`deploy.yml` `--from-file`) |
| `../scripts/pg-restore-verify.sh` | 🔴 geri yüklenmiş bir veritabanının **kaynakla aynı** olduğunu ölçer | operatör, geri yüklemenin son adımı |
| `../scripts/verify-image.sh` | push öncesi imaj kapısı (izlenebilirlik/tzdata/goose/uid); push sonrası `--access`: CI kimliğiyle çekilir, kimliksiz reddedilir (OPS-2) | `deploy.yml` |

Rol ayrımının tanımı **tek yerdedir**: `scripts/db-init/01-roles.sql`. `deploy.yml`
ConfigMap'i o dosyadan `--from-file` ile üretir; üretim için ikinci bir kopya
**yoktur**.

🔴 **`01-roles.sql` `tappa_app`'i `NOLOGIN` ve parolasız yaratır.** Girişi açan iki
dosya var ve ikisi de onun dışında: üretimde `deploy/k8s/postgres-init/02-app-password.sh`
(Secret'tan), geliştirmede `scripts/db-init/02-dev-only-password.sh` (sabit `tappa`).
İkincisi, `TAPPA_APP_PASSWORD` set ise **açılışta reddeder** — yani üretimde
yanlışlıkla mount edilirse sessizce değil **gürültülü** patlar. Gerekçe: eskiden rol
repodaki `'tappa'` parolasıyla yaratılıp sonra değiştiriliyordu ve değiştiren script
düşerse `PGDATA` dolu kaldığı için sonraki başlangıçta **repo parolası canlıda
kalıyordu** (denetimde ölçüldü).

🔴 **`kubectl apply -f deploy/k8s/` ÇALIŞTIRMA. Bir deploy yolu DEĞİLDİR.**

Bu cümle bir tur boyunca *"artık güvenlidir"* diyordu ve **iki şekilde yanlıştı** —
denetim ölçtü (2026-08-15). Sayı yanlıştı ve daha kötüsü *"güvenli"* kelimesi
Secret'tan **fazlasını** iddia ediyordu. O komutun bugün gerçekte yaptığı
(`kubectl apply --dry-run=client -f deploy/k8s/`, yeniden ölçüldü 2026-08-16 FAZ E:
**15 nesne, hiçbiri Secret** — `50-backup.yaml` geldiği için sayı 14'ten 15'e çıktı;
küme kapsamlı olanlar **hâlâ 3**, çünkü `CronJob` namespace'li):

> ⚠️ **Burada bir tur boyunca *"ve beşi küme kapsamlı RBAC"* yazdı ve BU YANLIŞTI**
> — üstelik bu paragrafın var olma sebebi **bir önceki yanlış sayıydı**, yani aynı
> kusur aynı cümlede ikinci kez doğdu. `kubectl api-resources`'ın kendi kapsam
> alanıyla ölçüldü: **14 nesnenin 3'ü küme kapsamlı** (`Namespace/tappa` ·
> `ClusterRole/tappa-namespace-only` · `ClusterRoleBinding/tappa-namespace-only`),
> 11'i namespace'li. ⚠️ **Bugün 15/3/12** — `50-backup.yaml`'ın `CronJob`'ı
> namespace'li, yani küme kapsamlı sayı **değişmedi**; ölçüm komutu
> `kubectl api-resources --namespaced=false -o name` ile üye kontrolü.
> ⚠️ **2026-10-09 (T116): 17/3/14 — ve bu sayı `kubectl` ile DEĞİL, dosyalardaki `kind:`
> satırları sayılarak bulundu** (ajan `kubectl` koşmaz; `grep -h '^kind:' deploy/k8s/*.yaml`).
> 15'ten 16'ya çıkışı `40-ingress.yaml`'ın ikinci Ingress'i (`tappa-redirects`, domain
> cutover 2026-09-02) bu paragrafı güncellemeden getirmişti; 17'ncisi `55-heartbeat.yaml`'ın
> `CronJob`'ı. İkisi de namespace'li, yani küme kapsamlı sayı **hâlâ 3**. `--dry-run=client`
> ölçümü orkestratörün küme adımlarında.
> ⚠️ 2026-10-10 (T112): `41-operator-ingress.yaml`'ın iki `Ingress`'i (namespace'li) →
> ⚠️ 2026-10-26 (M14-10a): `40-ingress.yaml`'ın üçüncü `Ingress`'i (`tappa-panel`, namespace'li) →
> **Güncel sayım: 21/3/18** (toplam / küme kapsamlı / namespace'li). Bu satır artık elle
> tutulmuyor: `TestPackaging_TheReadmeCountsTheManifestObjects` dosyalardaki sütun-0 `kind:`
> satırlarını sayar (yukarıdaki `grep`'in aynısı), türleri kapsamına göre ayırır ve bu çapayı
> arar — bir manifest eklenip sayı güncellenmezse kırmızı. `--dry-run=client` ölçümü yine
> orkestratörün.
> `01-rbac.yaml`'ın getirdiği **beş** nesnenin **yalnız ikisi**
> küme kapsamlı; **dördü** `rbac.authorization.k8s.io/v1` grubunda ve `ServiceAccount`
> düz `v1`. Yani *"beş yeni nesne"* ile *"beş küme-kapsamlı nesne"* birbirine
> karışmıştı. Bu listenin kendi cümlesi geçerli: **yanlış sayılmış bir açık,
> kapanmadığı sanılan bir açıktır.**

| | |
|---|---|
| örnek `Secret`'ı ezer mi | **hayır** — örnekler `deploy/examples/`'da (bu doğruydu) |
| RBAC | **uygular** — ve bunu yapabilmesi için **cluster-admin** kubeconfig gerekir; deploy'un kendi kimliğinde `rbac.authorization.k8s.io` yetkisi **yoktur** |
| `tappa-db-init` ConfigMap'i | **üretmez** — onu yalnız `deploy.yml` `--from-file` ile kurar → `tappa-postgres-0` kalıcı `ContainerCreating` (`configmap "tappa-db-init" not found`) |
| imaj etiketleri | **`:deploy-placeholder`** kalır → `ImagePullBackOff`, migration Job'ı **Failed** |
| çalışan bir kurulumda | rollout'u **kilitler** — Deployment'ı olmayan bir etikete geri alır |
| `50-backup.yaml`'ın CronJob'ı | **uygulanır, ve iki eksikle**: `configmap/tappa-backup-scripts` (onu `deploy.yml` kurar) ve `secret/tappa-backup-target` (operatörün) yoksa gece 02:30'da pod `ContainerCreating`'de kalır ve Job `activeDeadlineSeconds: 3600` dolunca **Failed** olur. Yani sessiz değil — ama *"yedeğim var"* sanılan bir CronJob bırakır |
| `55-heartbeat.yaml`'ın CronJob'ı | **uygulanır**; `secret/tappa-alert-pings` yoksa her 5 dakikada bir pod `CreateContainerConfigError`'da kalır, Job `activeDeadlineSeconds: 60` dolunca **Failed** olur ve healthchecks.io'ya sinyal gitmez — yani servis **uyarır** (gürültülü; adım 10) |

Yani boş kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır.
⚠️ Bu teorik değil: bu turda kümede kazara bırakılmış tam olarak bu durum bulundu ve
temizlendi (bozuk StatefulSet + `ImagePullBackOff` + Failed Job + bağlı 20Gi PVC).

**Elle deploy etmek gerekiyorsa** `.github/workflows/deploy.yml`'in adımlarını sırayla
izle: namespace → **`01-rbac.yaml`** (bir kez, cluster-admin ile) → `tappa-secrets`
(elle) → `harbor-pull` (elle — operatör adımı 4 (c); OPS-2) → ConfigMap'ler (`--from-file` dahil) → Postgres → NetworkPolicy → migration
Job (**bekle**) → Deployment → Ingress'ler (`40`; rollout'u bekle; sonra `41` — `41` yalnız
operatör yüzeyi yapılandırılmışsa; *"Uygulandı: K2"*, T119). Tek tek `apply` etmek güvenlidir; **dizini toptan**
`apply` etmek değildir.

---

## Operatörün yapması gerekenler (sırayla, bir kez)

**1. DNS — ve bu adım en kritik olanı.**

`taptime.mt` için **A kaydı → `144.76.158.60`**, Cloudflare proxy
**KAPALI** (gri bulut / "DNS only").

> ⚠️ `everva.com.tr` Cloudflare'de ve **proxy'li bir joker kayıt HÂLÂ var** —
> yeniden ölçüldü (2026-08-16): uydurma bir alt alan **hâlâ**
> `172.67.181.173`/`104.21.72.109`'a çözülüyor, yani bu adım **yeni bir host için
> hâlâ geçerli ve hâlâ varsayılanı yanlış**.
> ✅ **Ama `taptime.mt` AYRI ve KONTROLLÜ bir zone** (operatörün Cloudflare
> hesabı, zone oluşturma 2026-09-02) — everva'nın joker kaydının
> aksine **dövüşülecek proxy'li bir joker yok**. apex `A → 144.76.158.60` ve
> `www CNAME → apex`, ikisi de **DNS-only** (gri bulut) yaratıldı. Oluşturmadan
> sonra doğrulandı: `dig +short taptime.mt` → **`144.76.158.60`** (origin, Cloudflare
> kenar adresi değil); burada bir kenar adresi (`104.21.*`/`172.67.*`) görünürse
> proxy geri açılmış demektir. Kapı: `scripts/verify-deployment.sh cloudflare taptime.mt`.
> Proxy açık kalırsa uygulama her istemciyi bir Cloudflare adresi olarak görür:
> §5'in IP kanıtı (100 güven puanının 50'si) **hiç kimse için** doğru olamaz ve
> panel giriş bütçesi tüm müşteriler için tek adrese çöker (backlog T30).
> Gerekçenin tamamı `k8s/40-ingress.yaml` başında.

Doğrulama (kayıt açıldıktan sonra):

- `dig +short taptime.mt`: 144.76.158.60 olmalı

```bash
dig +short taptime.mt
curl -sS -o /dev/null -D - https://taptime.mt/ | grep -i 'cf-ray\|^HTTP/'
```

cf-ray varsa kayıt hâlâ proxy'li demektir; YOKSA yalnız `HTTP/2 200` satırı çıkar. (Desende `^HTTP/`
var, `^server` yok: origin `server` başlığı GÖNDERMİYOR ve durum satırında `HTTP` ile `/` arasında
iki nokta yok — eski desen sağlıklı durumda hiçbir şey basmayıp exit 1 veriyordu, yani "komut bozuk"
gibi görünüyordu.)

**2. Sırlar.** İkisinden birini seç — `examples/externalsecret.example.yaml`
**önerilir** (gerekçesi dosyanın başında: `TAPPA_TAG_KEK` bir dosyadan, kabuk
geçmişinden ve terminal tamponundan geçmez).

🔴 **Emanet (escrow) adımını atlama.** Beş değerin dördü yeniden üretilebilir;
`TAPPA_TAG_KEK` **üretilemez** — parktaki her plaketin AES anahtarını o sarmalıyor
(`tags.aes_key_ref`). Kaybolursa tek kurtarma yolu **her duvardaki her plaketi
fiziksel olarak yeniden encode etmek**. Değer kümenin dışında, dayanıklı ve erişimi
denetlenen bir yerde de bulunmalı (Infisical projesi, parola yöneticisi ya da kasada
mühürlü zarf). *"Küme'de duruyor"* emanet değildir: tek node'da, yedeksiz tek bir
etcd'dir.

üretim (yalnız bu iki komut, çıktıyı bir yere yapıştırmadan):

- `openssl rand -base64 32`: TAPPA_SESSION_HMAC_KEY / TAPPA_TAG_KEK / TAPPA_INVITE_HMAC_KEY
- `openssl rand -hex 32`: POSTGRES_PASSWORD / TAPPA_APP_PASSWORD  (URI'ye giriyorlar)

```bash
openssl rand -base64 32
openssl rand -hex 32
```

`TAPPA_INVITE_HMAC_KEY`, `TAPPA_SESSION_HMAC_KEY`'den **farklı** üretilmeli —
`config.Load` eşitlerse açılışta reddeder.

**3. Deploy'un kendi kimliği — `k8s/01-rbac.yaml` (bir kez, cluster-admin ile).**

- `kubectl apply -f deploy/k8s/00-namespace.yaml`: namespace önce; RBAC onun içine giriyor

```bash
kubectl --context hetzner-k8s-1 apply -f deploy/k8s/00-namespace.yaml
kubectl --context hetzner-k8s-1 apply -f deploy/k8s/01-rbac.yaml
```

🔴 **Bunu `deploy.yml` YAPAMAZ ve yapmamalı.** Bir Role'ü değiştirmek
`rbac.authorization.k8s.io` üzerinde yetki ister; deploy kimliğinde o yetki **yok**.
Kendi rolünü genişletebilen bir deploy kimliği sınır değil, formalitedir.

⚠️ **`apply` et, `delete` + yeniden yaratma.** `KUBE_CONFIG` secret'ı **bu**
ServiceAccount için üretilmiş bir token taşıyor; SA'yı silmek o token'ı geçersiz kılar
ve sıradaki deploy ilk küme çağrısında düşer. `apply` mevcut nesneleri **yamalar**.

⚠️ **Bu dosya var olan BEŞ nesnenin yerini alıyor** (backlog T44 üç tane sayıyordu;
ölçüm beş dedi). `ServiceAccount`, `Role`, `RoleBinding`, **`ClusterRole`** ve
**`ClusterRoleBinding`** (`tappa-namespace-only`; `namespaces`,
`resourceNames: [tappa]`) bugüne kadar **yalnız kümede**
vardı (elle, `2026-08-15T20:15:45Z`) — repoda hiçbir izi yoktu, yani taze bir küme
deploy'u koşturamazdı ve **hiçbir denetim bu rolü görmedi**. Dosyanın başındaki blok
her yetkinin **hangi `deploy.yml` adımı** için orada olduğunu, ve çıkarılan her
yetkinin **hangi ölçümle** gereksiz bulunduğunu yazıyor (backlog T44).

🔴 **Elle rol, dosyaya göre GENİŞTİ ve bu §4.7'ye değiyordu:** `ns/tappa`'da
`secrets`'a **tam CRUD** veriyordu, yani `tappa-secrets` — içinde `TAPPA_TAG_KEK` —
bir iş akışının patlama yarıçapındaydı ve bunu yazan bir dosya yoktu. Yeni dosyada
`secrets` yetkisi **adlı nesneler üzerinde tek fiil**: `get` / `tappa-secrets`, ve OPS-2'den beri
`harbor-pull` (Harbor `k8s-pull` robotunun çekme kimliği — *"Preflight — the registry pull
Secret…"* varlığını ölçer; `get` nesneyi verisiyle döndürdüğü için bu kimlik o robotun değerini
**okuyabilir** — yalnız çekme, tek proje; iş akışı zaten itip çekebilen `robot$tappa-ci`'yi taşıyor).
🔴 **OPS-2'den sonra bu dosya bir kez daha `apply` edilmeli**, yoksa o preflight `Forbidden` ile
durur.
**Ne KAPANMADI, sayılıyor:** deploy kimliği bir `Job` yaratabilir (migration'ın
kendisi budur) ve bir Job'ın pod şablonu **kendi namespace'indeki her Secret'a**
referans verebilir — bu referans RBAC istemez. Kum havuzunda uçtan uca ölçüldü:
bu kurallarla yaratılan bir Job, listeleyemediği bir Secret'ın değerini okuyup
loguna bastı. Yani daraltma **okumayı** değil, **yıkıcı yarıyı** kaldırıyor
(artık `tappa-secrets`'ı yaratamaz, ezemez, silemez). Ayrıntı: sınır
**[deploy kimliği namespace'in sırlarını okuyabilir]**.

**4. Harbor — private projeler `tappa` ve `dockerhub`, iki robot, `harbor-pull` Secret'ı (OPS-2).**

🔴 **Bu adım operatörün ve deploy onsuz koşmaz.** İmajlar Docker Hub'dan (public,
`docker.io/atknatk/tappa` ve `…/tappa-migrate`) projenin kendi Harbor'ına taşındı:
**`registry.everva.com.tr`** (Harbor v2.15.2, k8s-2'de koşar), **private** proje **`tappa`**.
Depolar: `tappa/tappa` (sunucu), `tappa/tappa-migrate` (migration), sandbox gelince
`tappa/tappa-sandbox`. Etiket saklama (tag retention): **her depoda son 30 push**, günlük 03:00
— geri dönüşün ne kadar geriye çekebileceğini bu sınırlar (sınır **[Harbor k8s-2'ye bağlı]**).

**İkinci proje: `dockerhub` — Docker Hub'ın proxy cache'i (private).** Harbor'daki registry
uç noktası Docker Hub'a bakar (endpoint `dockerhub`), proje onun önbelleğidir:
`registry.everva.com.tr/dockerhub/library/<imaj>` Docker Hub'ın `library/<imaj>`'ını Harbor
üzerinden verir. Derlemenin taban imajı buradan, **digest'iyle** gelir (*"Build both images"*:
`GO_BUILD_IMAGE=registry.everva.com.tr/dockerhub/library/golang:1.26.10-bookworm@sha256:5b1f78db…b505`,
`--build-arg GO_IMAGE` olarak iki derlemeye; 2026-10-21'de GO-2026-6809 için 1.26.9'dan
—`d9c68c2c…453c`— taşındı, index'in linux/amd64 çocuğu `sha256:88750ccdd0f2…f9d9`; aşağıdaki
2026-10-19 ölçüleri 1.26.9 digest'iyle yapıldı). Digest içerik adresidir: proxy ya **tam bu** index'i
verir ya da düşer, Docker Hub'da yeniden itilmiş bir etiket iki derlemeyi de değiştirmez.
`Dockerfile`'ın varsayılanı **aynı digest**, Docker Hub'ın kendi adıyla
(`golang:1.26.10-bookworm@sha256:…`): yerel ve sandbox derlemesi Harbor kimliği taşımaz, proxy projesi
private. `ci.yml` Dockerfile'ı hiç derlemez (araç zinciri `setup-go`, aynı sürüm), ona bir şey gerekmez.
Bu proje OPS-2'nin ilk turunda açık kalan riski kapatır: deploy Docker Hub'a artık giriş yapmadığı için
taban imaj runner'ın paylaşılan adresinden **anonim** çekiliyordu (`ci.yml`'in 2026-10-09'da
`toomanyrequests` ölçtüğü bütçe).

**Neden private bir depo bu sefer T43'ü geri getirmiyor.** T43'ün ölçülmüş kökü bu node'un
kubelet'inin `imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages` ile koşmasıydı
(KEP-2535): **kimlikle** çekilmiş bir imaj, yalnız o kimliği sunan bir pod'a önbellekten verilir;
başkası bir **doğrulama çekmesi** öder. O günün kimliği `secret/ghcr`'dı ve onu **her deploy kendi
`GITHUB_TOKEN`'ıyla yeniden yazıyordu** — token iş bitince ölüyordu, yani kaydedilen her kimlik bir
saat içinde ölüydü. O gün çare public depoydu (kaydedilecek kimlik yok). Bugün değişen **yazan**:
`harbor-pull`'u CI **yazmaz** (deploy kimliğinin sırlar üzerinde yalnız `get`'i var, iki isimde —
adım 3), Secret yalnız operatör robotu döndürdüğünde değişir. Kalan koşullar (bu kümede henüz
**ölçülmedi** — ölçü *"OPS-2 kapanış ölçüsü"*nde):

- Robotun secret'ı **değişmedikçe** yeniden başlayan bir pod ya da `rollout undo` düğümdeki kopyadan
  açılır, Harbor'a gitmez.
- Robot **döndürülünce** (ya da Secret yeniden yaratılınca), eski değerle çekilmiş bir imaja dönen pod
  bir doğrulama çekmesi öder: Harbor ayaktaysa ve yeni değer geçerliyse sorunsuz, değilse
  `ImagePullBackOff`. Aşağıdaki (d) bu pencereyi açmadan döndürür.
- **Yeni** bir imaj her zaman Harbor ister — k8s-2 kapalıyken deploy yeni pod başlatamaz (ve taban
  imajı proxy'den çektiği için **derleyemez** de).

**(a) İki robot — ikisi de süresiz (Expiration: Never).** Değerler Infisical'da:
`shared-infra/prod:/tappa/HARBOR_CI_ROBOT_NAME`, `…/HARBOR_CI_ROBOT_SECRET`,
`…/HARBOR_PULL_ROBOT_NAME`, `…/HARBOR_PULL_ROBOT_SECRET`. Değer bu belgeye **yazılmaz**.

| Robot | Tür | İzinler | Kim kullanır |
|---|---|---|---|
| `robot$tappa-ci` | **sistem** robotu (Administration → Robot Accounts), iki proje | `tappa`: repository push + pull, artifact read + list, tag create + list · `dockerhub`: repository pull, artifact read | `deploy.yml` — `production` ortamının `HARBOR_CI_ROBOT_NAME` / `HARBOR_CI_ROBOT_SECRET`'ı: push, iki yönlü kapı ve taban imajın çekilmesi |
| `robot$tappa+k8s-pull` | **proje** robotu (`tappa`) | `tappa`: repository pull, artifact read | küme — `harbor-pull` (ns `tappa`; sandbox gelince ns `tappa-sandbox` da) |

Ölçüldü (ana oturum, 2026-10-19, registry token kapsamlarıyla): `robot$tappa-ci` → `tappa` push+pull,
`dockerhub` pull; `k8s-pull` → yalnız `tappa` pull; anonim → `401`; proxy'den
`dockerhub/library/golang:1.26.9-bookworm` manifest'i `200`. OPS-2'nin ilk turundaki proje robotu
`robot$tappa+ci` **silindi** — tek proje görüyordu, proxy projesine erişemezdi.
**Süresiz olmanın bedeli:** takvim bir robotu kendiliğinden kapatmaz; döndürme bir **karardır**
(sızıntı şüphesi, ekipten ayrılan biri) ve aşağıdaki (d)'nin yoludur. Bir robot Harbor'da
**devre dışı** bırakılır ya da silinirse: `ci` için deploy *"Log in to the registry"* adımında, `k8s-pull`
için yeni pod'lar `401`'de düşer (*"Olay müdahalesi"*, 2).

**Yeniden üretmek.** Harbor arayüzü: **`ci`** için **Administration → Robot Accounts → New Robot
Account**, adı `tappa-ci`, Expiration **Never**, iki proje satırı (`tappa` ve `dockerhub`) yukarıdaki
izinlerle; **`k8s-pull`** için **Projects → `tappa` → Robot Accounts → New Robot Account**, adı
`k8s-pull`, Expiration **Never**. Arayüz yoksa aynı iş Harbor API'siyle (`POST /api/v2.0/robots`;
`ci` için `level: system` ve iki `{"kind":"project","namespace":…,"access":[…]}`, `k8s-pull` için
`level: project`), yönetici kimliğiyle, SSH ile k8s-2'ye bağlanıp `docker exec -i harbor-core curl …`
ile kabın içinden — aşağıdaki yenileme bloğunun kalıbıyla (yönetici parolası sunucuda okunur, curl'e
stdin'den gider). Yeni robotun secret'ı **bir kez**
gösterilir: doğrudan Infisical'a, sonra (b) ya da (c).

**Robot secret'ını yenilemek — değer basmadan.** Arayüz: robotun satırında **⋮ → Refresh secret**;
Harbor yeni değeri bir kez gösterir — ekrandan Infisical'a kopyala, sonra (b) (`ci`) ya da (c)
(`k8s-pull`). 🔴 **Refresh eski değeri HEMEN geçersiz kılar:** `ci` için zararsız (sıradaki deploy
yeni değeri okur — önce (b), sonra Refresh), `k8s-pull` için **kullanma**, (d)'nin yeni-robot yolunu
izle. API (arayüze erişilemezse; bu repodan **koşulmadı** — blok testte taklit bir sunucuya karşı
koşulur): Harbor k8s-2'de **docker compose** ile koşar, çekirdek API'si `harbor-core` kabının içinde
`http://127.0.0.1:8080/api/v2.0`'dadır ve k8s-2'de **kubectl bağlamı yoktur** — istek SSH ile k8s-2'ye,
oradan `docker exec -i harbor-core curl …` ile kabın içine gider. Yönetici kimliği **sunucuda** okunur
(`/opt/harbor/harbor.yml`, `harbor_admin_password`) ve sunucudan çıkmaz: değer hiçbir argv'ye girmez
(`printf` kabuğun kendi komutudur), curl'e **stdin**'den yapılandırma dosyası olarak gider (`-K -`;
`"` ve `\` kaçırılır), ekrana basılmaz. Gövde `{}`'dir — Harbor yeni secret'ı **kendisi** üretir ve
cevapta bir kez döndürür (`PATCH /api/v2.0/robots/<id>`, `{"secret":"…"}`); cevap SSH'tan doğrudan
yerel, yalnız sana açık bir dosyaya yazılır, ekrana yalnız HTTP kodu gelir, secret `$H/secret`'a
ayıklanır ve cevap dosyası silinir. Önce iki değişkeni tanımla: `K2` (k8s-2'nin SSH hedefi — örn.
`~/.ssh/config`'teki takma adı; harbor.yml'i okuyabilen ve docker koşturabilen bir kullanıcıyla,
pratikte root; adres ve anahtar bu belgeye **yazılmaz**) ve `ROBOT_ID` (robotun Harbor kimliği, yalnız
rakam — `GET /api/v2.0/robots` listesindeki `id`; aşağıdaki ilk blok onu bulur). Biri eksikse ya da
`ROBOT_ID` rakamdan başka bir şey taşıyorsa SSH **hiç açılmaz**. ⚠️ `harbor.yml`'deki parola
**kurulum** parolasıdır: yönetici parolası sonradan arayüzden değiştirildiyse istek `401` alır (eski
secret yerinde kalır) — o zaman arayüz yolu.

`ROBOT_ID`'yi bulmak — aynı kalıp, değer basmadan: liste SSH'tan doğrudan yerel, yalnız sana açık bir
dosyaya iner, ekrana **yalnız** her robotun `id`'si ve adı gelir (`3 robot$tappa-ci` biçiminde), dosya
silinir. Robot listesi secret taşımaz (Harbor secret'ı yalnız yaratma ve yenileme cevabında döndürür),
ama kural aynı: ham cevap ekrana gelmez. Liste okunamazsa (`K2` yok, SSH, `harbor.yml`, `401`) tek satır
`no robot list` der.

```bash
umask 077
L=$(mktemp -d)
[ -n "${K2:-}" ] && ssh -T "$K2" sh -s > "$L/robots" <<'EOF'
set -eu
p=$(sed -n 's/^harbor_admin_password[:][[:space:]]*//p' /opt/harbor/harbor.yml | head -n 1 | sed -e 's/[[:space:]]*$//' -e "s/^\"\(.*\)\"\$/\1/" -e "s/^'\(.*\)'\$/\1/")
[ -n "$p" ] || { echo 'no harbor_admin_password in /opt/harbor/harbor.yml' >&2; exit 1; }
printf 'user = "admin:%s"\n' "$(printf '%s' "$p" | sed 's/[\\"]/\\&/g')" | docker exec -i harbor-core curl -q -sS -f --max-time 30 -K - 'http://127.0.0.1:8080/api/v2.0/robots?page_size=100'
EOF
grep -o '"id":[0-9]*' "$L/robots" 2>/dev/null | cut -d: -f2 > "$L/ids"
grep -o '"name":"[^"]*"' "$L/robots" 2>/dev/null | cut -d'"' -f4 > "$L/names"
[ -s "$L/ids" ] && [ "$(wc -l < "$L/ids")" -eq "$(wc -l < "$L/names")" ] && paste -d ' ' "$L/ids" "$L/names" || echo "no robot list (K2 unset, ssh, harbor.yml or the API failed)"
rm -rf "$L"
```

Sonra `ROBOT_ID=<id>` (yenilenecek robotun satırından) ve aşağıdaki yenileme bloğu.

```bash
umask 077
H=$(mktemp -d)
[ -n "${K2:-}" ] && [ -n "${ROBOT_ID:-}" ] && [ -z "$(printf '%s' "$ROBOT_ID" | tr -d 0-9)" ] && ssh -T "$K2" sh -s "$ROBOT_ID" > "$H/resp" <<'EOF'
set -eu
p=$(sed -n 's/^harbor_admin_password[:][[:space:]]*//p' /opt/harbor/harbor.yml | head -n 1 | sed -e 's/[[:space:]]*$//' -e "s/^\"\(.*\)\"\$/\1/" -e "s/^'\(.*\)'\$/\1/")
[ -n "$p" ] || { echo 'no harbor_admin_password in /opt/harbor/harbor.yml' >&2; exit 1; }
printf 'user = "admin:%s"\n' "$(printf '%s' "$p" | sed 's/[\\"]/\\&/g')" | docker exec -i harbor-core curl -q -sS --max-time 30 -K - -X PATCH -H 'Content-Type: application/json' --data '{}' -w '\n%{http_code}\n' "http://127.0.0.1:8080/api/v2.0/robots/$1"
EOF
C=$(tail -n 1 "$H/resp" 2>/dev/null); echo "refresh: HTTP ${C:-none}"
[ "$C" = 200 ] && sed -n '1s/.*"secret":"\([^"]*\)".*/\1/p' "$H/resp" | tr -d '\n' > "$H/secret" && [ -s "$H/secret" ] && echo "the new secret is in $H/secret" || { rm -f "$H/secret"; echo "no new secret: HTTP 200 means refreshed but unreadable (refresh again), any other code means the old secret still holds"; }
rm -f "$H/resp"
```

`refresh: HTTP 200` ve `the new secret is in …` beklenir; `HTTP none` SSH'ın hiç açılmadığını ya da
sunucu tarafının düştüğünü (`K2`/`ROBOT_ID` eksik, harbor.yml okunamadı, kap yok), başka bir kod eski
değerin **yerinde** kaldığını söyler. Yeni değer `$H/secret`'tadır ve oradan **stdin**'le gider —
`gh`'ye dosyadan, Infisical'a panodan (`shared-infra/prod:/tappa/HARBOR_CI_ROBOT_SECRET`):

```bash
[ -s "$H/secret" ] && gh secret set HARBOR_CI_ROBOT_SECRET --env production --repo atknatk/tappa < "$H/secret" && pbcopy < "$H/secret" && echo "set in production; on the clipboard for Infisical"
```

Infisical'a yapıştırdıktan sonra panoyu ve dizini temizle:

```bash
pbcopy < /dev/null
rm -rf "$H"
```

⚠️ **Sınır:** `pbcopy < /dev/null` yalnız panonun **o anki** içeriğini boşaltır. Bir pano geçmişi
uygulaması (Raycast, Maccy, Alfred, Paste …) değeri kendi geçmişinde tutar ve **Universal Clipboard**
açıksa değer aynı Apple hesabındaki öteki cihazlara da gitmiştir — bu komut ikisini de temizlemez.
Bunlardan biri açıksa yenilemeden **önce** duraklat (Universal Clipboard: System Settings → General →
AirDrop & Handoff → Handoff kapalı) ya da değeri o uygulamanın geçmişinden elle sil.

**(b) CI robotu → `production` ortamı.** Komut değeri gizli bir istemden okur; değeri komut satırına
yazma, ekrana basma. Robot adındaki `$` istemde sorun değildir (`robot$tappa-ci`).

```bash
gh secret set HARBOR_CI_ROBOT_NAME --env production --repo atknatk/tappa
gh secret set HARBOR_CI_ROBOT_SECRET --env production --repo atknatk/tappa
```

**(c) `harbor-pull` — kümenin çekme kimliği, operatörün Secret'ı.** **Bugün var** (ana oturum,
2026-10-19, ns `tappa`, `kubernetes.io/dockerconfigjson`, `k8s-pull` robotu). CI onu **oluşturmaz**
(`tappa-secrets` kalıbı); `deploy.yml` her koşuda, kümeye bir şey uygulamadan önce **varlığını,
tipini ve `registry.everva.com.tr` için bir kimlik taşıdığını** ölçer (*"Preflight — the registry
pull Secret is the operator's and must already exist"* = `scripts/verify-deployment.sh
pull-secret`; değer basılmaz). Yoksa kırmızı ve adıyla; okunamıyorsa (`Forbidden`) kırmızı ve
**adım 3'ü yeniden uygula** der — `01-rbac.yaml` OPS-2'den beri `harbor-pull`'a `get` veriyor.

Yeniden yaratmak (tailnet'teki bir makineden). 🔴 **`docker login` KULLANILMAZ — iki ölçülmüş sebeple
(OPS-2 düzeltme turu 1).** (1) macOS'ta docker CLI, **boş** bir `DOCKER_CONFIG` dizininde bile
kimliği Docker Desktop'ın **keychain** yardımcısına yazar (dizinde kimlik yoksa varsayılan depoyu
kendisi seçer): kimlik keychain'e gider, `config.json`'a **gitmez**, ve oradan üretilen Secret
kimliksiz bir kopyayla **çalışan** `harbor-pull`'u ezebilir. (2) `--password-stdin`'e yapıştırılan
değer etkileşimli terminalde **görünür**. Bu yüzden blok `config.json`'u **kendisi** yazar: secret
`read -rs` ile (yankısız) okunur, `printf` ve `read` kabuğun kendi komutlarıdır, değer hiçbir
sürecin argv'sine girmez ve yalnız dosyaya yazılır. Sonra **o dosyayla** prod'un koştuğu imajın
manifest'i okunur — `config.json`'da kimlik **varken** docker CLI yardımcıya bakmaz, dosyadakini
kullanır (ölçüldü, 2026-10-19, docker 29.8.2, yerel bir registry'ye karşı: istek dosyanın kimliğini
taşıdı) — ve Secret **ancak o okuma geçerse** yazılır; imaj Harbor'da değilse (OPS-2 öncesi
`docker.io/...`) doğrulama yapılmaz ve hiçbir şey yazılmaz. 🔴 **`apply` DEĞİL, `create` /
`replace`** (OPS-2 düzeltme turu 2): istemci tarafı `apply` nesnenin tamamını
`kubectl.kubernetes.io/last-applied-configuration` açıklamasına kopyalar — Secret'ın verisi
metadata'da **ikinci kez** durur ve Secret'ın metadata'sını okuyan her araç onu görür (adım 10
(a)'nın `tappa-alert-pings` için koyduğu kuralın aynısı). Blok önce Secret'ın **adını** okur (değer
değil): yoksa `create`, varsa `replace` — `replace` nesneyi **bütünüyle** yeniden yazar, yani eski
bir `apply`'ın bıraktığı açıklama da gider (canlı `harbor-pull`'daki açıklamayı ana oturum
2026-10-19'da sildi). İsim okunamazsa (bağlam yok, `Forbidden`) blok `create` dener: var olan bir
Secret'ı `create` ezmez, `AlreadyExists` ile düşer — bu Kubernetes API sunucusunun davranışıdır,
bloğun stub'lı koşusu onu sınamaz (stub kubectl ne cevap verirse o olur). Dosya en sonda silinir. `N` hedef
namespace'tir; sandbox için `N=tappa-sandbox` (pod bir `imagePullSecret`'ı yalnız **kendi**
namespace'inden okur — doğrulama yine prod'un imajıyla yapılır, robot aynı).

```bash
umask 077
N=tappa
D=$(mktemp -d)
U='robot$tappa+k8s-pull'
printf 'k8s-pull robot secret: '; read -rs S; echo
printf '{"auths":{"registry.everva.com.tr":{"auth":"%s"}}}\n' "$(printf '%s:%s' "$U" "$S" | base64 | tr -d '\n')" > "$D/config.json"
unset S
I=$(kubectl --context hetzner-k8s-1 -n tappa get deployment tappa -o jsonpath='{.spec.template.spec.containers[0].image}')
V=create; [ -n "$(kubectl --context hetzner-k8s-1 -n "$N" get secret harbor-pull --ignore-not-found -o name)" ] && V=replace
[[ $I == registry.everva.com.tr/tappa/* ]] && DOCKER_CONFIG="$D" docker manifest inspect "$I" > /dev/null && kubectl --context hetzner-k8s-1 -n "$N" create secret generic harbor-pull --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson="$D/config.json" --dry-run=client -o yaml | kubectl --context hetzner-k8s-1 -n "$N" "$V" -f - && echo "harbor-pull written in $N ($V)" || echo "harbor-pull NOT written: the credential did not read $I, or $V failed"
rm -rf "$D"
scripts/verify-deployment.sh pull-secret "$N" harbor-pull registry.everva.com.tr
```

Son satır `harbor-pull written in tappa (create)` / `(replace)` ya da `NOT written` der; ikincisinde
Secret **değişmemiştir** (yanlış değer, devre dışı robot, Harbor'a ulaşılamıyor, imaj Harbor'da değil
ya da `create`/`replace` reddedildi). Son komut —
`scripts/verify-deployment.sh` **kubectl'in geçerli bağlamını** okur, `--context` almaz — bağlam
`hetzner-k8s-1` değilse başka bir kümeyi okur; önce `kubectl config current-context`.

**(d) Döndürme — pencere açmadan.** Harbor'da **Refresh secret** eski değeri **hemen** geçersiz
kılar; `ci` için bu zararsızdır (sıradaki deploy yeni değeri okur — önce (b), sonra Harbor), ama
`k8s-pull` için Secret güncellenene kadar her doğrulama çekmesi `401` olur. Bu yüzden `k8s-pull` şöyle
döndürülür: (1) Harbor'da **yeni** bir robot (`k8s-pull2`, aynı izinler, Expiration Never) — eski robot
**çalışmaya devam eder**; (2) (c)'nin bloğu yeni robotun adı (`U`) ve değeriyle (Secret var, blok
`kubectl replace` seçer ve onu **yerinde** günceller; doğrulama geçmezse yazmaz); (3) bir deploy koşusu ya da
`kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa` + `rollout status` —
pod'lar yeni kimlikle doğrulanır; (4) eski robotu sil, Infisical'ı güncelle.

**(e) İki yönlü kapı — talimat değil, mekanizma.** `deploy.yml` push'tan hemen sonra, kümeye
**hiçbir şey uygulamadan önce** iki imajı da iki kez ister (*"Gate — both images must be pullable
WITH the CI credential and NOT without one"* = `scripts/verify-image.sh --access`): **CI kimliğiyle**
(`docker manifest inspect`) manifest gelmeli (`"schemaVersion"`, yalnız exit 0 değil); **kimliksiz**
registry **reddetmeli** — ve bu yarı docker'la değil, **curl'le, Registry v2 token akışıyla**
ölçülür (OPS-2 düzeltme turu 1): docker 29 Harbor'ın kimliksiz `401`'ini *"no such manifest"* diye
bildirir (ölçüldü, 2026-10-19, docker 29.8.2, Harbor gibi cevap veren yerel bir registry'ye karşı),
yani docker'ın sözlerini okuyan ilk sürüm **her koşuda** kırmızıydı. Akış: `/v2/` → `401` ve bir
`Bearer realm=…,service=…` çağrısı; o realm'den kimliksiz bir token (`scope=repository:<depo>:pull`
— public bir depo bu token'la okunurdu); manifest o token'la → **`401` ve hata kodu
`UNAUTHORIZED`** = beklenen ret. `200` (proje public) **kırmızı**; başka her sonuç — 5xx, DNS, zaman
aşımı, boş cevap, token vermeyen bir token servisi, `UNAUTHORIZED` taşımayan bir `401` — da
**kırmızı**: registry'nin reddettiğini gösteremez. curl `-q` ile (hiçbir `~/.curlrc` kimlik ekleyemez)
ve `--proto =https` ile koşar. Sonra *"Log out of the registry"* kimliği diskten siler; kubectl ve
tailnet ondan sonra gelir.
**Neden `k8s-pull` ile değil:** kümenin kimliğini CI'ya taşımak, iş akışının patlama yarıçapına ikinci
bir kimlik koyardı ve tek bir ölçüm alırdı (`k8s-pull` hâlâ çalışıyor mu) — onu küme zaten ölçer:
migration Job'ı sunucudan **önce** çeker; çekemezse pod `ImagePullBackOff`'a düşer, *"Migrate"* adımı
`ImagePullBackOff`'u **gördüğü ilk** ankette kırmızı biter (OPS-2 düzeltme turu 1; önceden 600 sn bekliyordu) ve
**sunucu rollout'u hiç yapılmaz** (*"Olay müdahalesi"*, 2). Bu açığın kalan parçası — Harbor'da
devre dışı bırakılmış bir `k8s-pull` — bir deploy'un kırmızı bitmesidir; kabul edildi.

**5. GitHub `production` ortamı — `KUBE_CONFIG` ve Tailscale OAuth client'ı (küme API'si yalnız tailnet'ten).**

🔴 **Küme API'si internete KAPALI (backlog T134, 2026-10-17).** Kullanıcı
`144.76.158.60:6443`'ü dışarıya **bilerek** kapattı; API'ye yalnız Tailscale üzerinden
ulaşılır. Kümedeki Tailscale **subnet router**'ı `10.43.0.0/16`'yı duyurur ve
Kubernetes API'si `kubernetes` Service'inin adresinde, **`https://10.43.0.1:443`**'te
cevap verir (orkestratör ölçtü: tailnet'ten erişilir, küme CA'sıyla doğrulanır, bearer
token `system:serviceaccount:tappa:github-deployer` olarak tanınır). `deploy.yml`'de
üç adım bu yolu taşır:

1. *"Write kubeconfig"* — kubeconfig'i yazar ve **çevrimdışı** denetler
   (`scripts/verify-deployment.sh kubeconfig-server`): **current context**'in sunucusu
   `https://10.43.0.1:443` (ya da `https://10.43.0.1`, sonda en çok bir `/`) olmalı,
   `proxy-url` **boş**, `insecure-skip-tls-verify` **boş ya da false**. Genel IP'yi,
   bir benzeri (`10.43.0.10`, `10.43.0.1.nip.io`), `http://`'yi, bir yolu, bir proxy'yi ya
   da atlanmış bir sertifika denetimini adıyla reddeder (kırmızı, kümeye dokunmadan).
2. *"Join the tailnet"* — son imaj adımından **sonra**, ilk küme çağrısından (*"Preflight —
   the application Secret…"*) hemen **önce**: `tailscale/github-action`, **tam commit
   SHA'sıyla** pinli (`d1b6cd2…`, v4.2.0; depodaki tek üçüncü-parti action — gerekçe
   dosyada), **OAuth client** (`oauth-client-id` / `oauth-secret` ← `TS_OAUTH_CLIENT_ID` /
   `TS_OAUTH_SECRET`, `tags: tag:ci` — OPS-2), **`version: "1.94.2"` + `sha256sum`**
   (indirilen arşiv bu dosyada pinli digest'le karşılaştırılır, aynı sunucudan alınanla
   değil — backlog T135; yükseltme (e)'de), `--accept-dns=false`, önbelleksiz (önbellek
   isabeti digest denetimini atlar),
   **`log-mode: quiet`** (hata ayıklama kipinde action'ın kendi `tailscale status --json`
   çıktısını — tailnet kullanıcılarının e-posta adreslerini — public log'a basmaması için),
   `timeout: 45s` × `retry: "3"` (action'ın varsayılanı 2 dk × 5 ≈ 10 dk bekleyişti; şimdi
   ≈ 2,5 dk).
3. *"Gate — the runner joined the tailnet as tag:ci and reaches the cluster API"*
   (`scripts/verify-deployment.sh tailnet`) — `Self.Tags`'ta **tam** `tag:ci` dizgesi yoksa
   (yok, `[]`, `tag:cix`, `tag:CI`, `" tag:ci"`, dizi yerine dizge) **kırmızı**;
   `10.43.0.1:443`'e TCP ile ulaşamazsa (6 deneme × `timeout 5`) **kırmızı**; status
   okunamıyorsa (CLI yok, boş, bozuk JSON, `NeedsLogin`, `Self` yok) **kırmızı**;
   cihaz kendini ephemeral **değil** (`false`) diye bildirirse **kırmızı** (OPS-2: OAuth yolu her
   koşuda ephemeral ister — `false`, katılımın o yoldan gitmediği demektir); **hiç** bildirmiyorsa
   **uyarı** — ve 1.94.2'nin status JSON'unda bu alan **yok** (`ipnstate.PeerStatus`; T134'ün ilk
   yeşil koşusu `ephemeral absent` bastı), yani bugün her koşu bu dala düşer: ephemeral **ölçülebildiği
   yerde zorunlu**, ölçülemediği yerde uyarıdır ve kalan ölçü koşu sonrası yönetim konsoludur
   (*"OPS-2 kapanış ölçüsü"*). Uyarıyı kırmızı yapmak her deploy'u kırmızı yapardı. İstemci sürümünü basar; status
   JSON'unu **hiçbir dalda basmaz** (`TestVerifyDeployment_TheTailnetModeMeasuresTheJoin`).

**(a) Tailscale OAuth client'ı — `TS_OAUTH_CLIENT_ID` + `TS_OAUTH_SECRET` (OPS-2; `TS_AUTHKEY`'in
yerini aldı).** Kullanıcı oluşturdu: kapsam **`auth_keys`**, etiket **`tag:ci`** (ölçüldü, 2026-10-19).
**Etiketi ve ephemeral'ı artık bu dosya ister, bir anahtarın nasıl üretildiği değil** — pinli
commit'te **koşan** koddan okundu (`dist/index.js`, `src/main.ts` aynı; *"2. OAuth client"* dalı):
OAuth secret'ı verilince action `tailscale up --advertise-tags=tag:ci … --authkey=<oauth secret>
?preauthorized=true&ephemeral=true` koşar (`tags` o yolda **zorunlu** — yoksa action atar); OAuth
secret'ını tek kullanımlık, ephemeral, ön onaylı bir anahtara **istemci** çevirir (tailscale 1.94.2,
`feature/oauthkey`'in `resolveAuthKey`'i — pinli tarball'daki ikilide `tskey-client-` ve *"oauth
authkeys require --advertise-tags"* dizgeleri okundu). `oauth-client-id` bu commit'te yalnız workload
identity yolunda okunur — OAuth-secret yolunda secret kendi client'ını taşır; satır action'ın belgelenen
çifti olduğu için duruyor, katılımı o sağlamıyor. Takvimde süre **yok** (auth key'in 90 günü kalktı).
⚠️ Auth key'den **güçlü** bir kimliktir: iptal edilene dek `tag:ci` anahtarı basabilir (`auth_keys`,
başka hiçbir şey); argv ve `tailscaled` ortamı yolu aynı — sınır **[deploy tailnet'e üçüncü-parti bir
action ve bir OAuth client'la girer]**.

| Özellik | Değer | Neden |
|---|---|---|
| Kapsam | **`auth_keys`** (yazma) | anahtar basmak için; başka kapsam verme |
| Etiket | **`tag:ci`** | client yalnız kendi etiketlerini atayabilir; `tags: tag:ci` başka bir etiket olsaydı anahtar isteği düşerdi |
| Ephemeral / ön onay | dosyada (`?ephemeral=true&preauthorized=true`) | her koşu yeni, ephemeral bir cihaz; kapı `false`'u kırmızı yapar |

Tailscale yönetim konsolu: Settings → OAuth clients → Generate OAuth client. Secret **bir kez**
gösterilir: Infisical'a, sonra ortama. Komutlar değeri gizli bir istemden okur; değeri komut satırına
yazma, ekrana basma. `deploy.yml`'in ilk adımı `TS_OAUTH_SECRET`'ın `tskey-client-` ile başladığını
da denetler (bir auth key yapıştırılırsa adıyla düşer; değer basılmaz).

```bash
gh secret set TS_OAUTH_CLIENT_ID --env production --repo atknatk/tappa
gh secret set TS_OAUTH_SECRET --env production --repo atknatk/tappa
```

Tailnet ACL'inde gereken (kullanıcı ekledi, 2026-10-17): `tagOwners`'ta `tag:ci` ve
`tag:ci` → `10.43.0.1:443` izni. Subnet router'ın `10.43.0.0/16` rotası **onaylı**
olmalı. **Tailnet Lock** açıksa yeni düğümler yalnız imzalı bir anahtarla katılabilir — OAuth'un
her koşuda bastığı anahtarın o durumda katılıp katılamadığı **ölçülmedi**; Tailnet Lock açılırsa bu
yol yeniden ölçülür.
⚠️ OAuth secret'ı `tailscale up`'ın **argv**'sine ve `tailscaled`'in **ortamına** girer (action
böyle geçiriyor) — sınır **[deploy tailnet'e üçüncü-parti bir action ve bir OAuth client'la
girer]** (b).

🔴 **Tailscale OPERATÖRÜNÜN API proxy'si (`*.ts.net`) KULLANILMAZ.** Auth kipinde çalışır:
bearer token'ı **yok sayıp** isteği Tailscale kimliğiyle taklit eder. Yani deploy,
`01-rbac.yaml`'ın dar `github-deployer` kimliğiyle değil, tailnet kimliğine bağlanmış
neyse onunla konuşurdu — adım 3'teki daraltma anlamsızlaşırdı. Sunucu doğrudan
`10.43.0.1`'dir, kimlik kubeconfig'teki token'dır.

**(b) `KUBE_CONFIG` — sunucusu `https://10.43.0.1:443`, CA'sı kümenin `kube-root-ca.crt`'si.**
Bu adres `kubernetes` Service'inin adresidir ve API sunucusunun sertifikası onu kapsar
(ölçüldü — yukarıda); CA, herhangi bir namespace'teki `kube-root-ca.crt` ConfigMap'inin
`ca.crt`'sidir. Üç blok, **sırayla**; ikinci ile üçüncünün arasında token'ı bir **editörle**
dosyaya yazarsın. (Blokları tek parça yapıştırmak, token'sız bir kubeconfig'i secret'a
yazardı — bu yüzden bölündüler.) Bloklar zsh'a yapıştırılmaya uygundur: bloklarda **hiç yorum
yok** (açıklamalar blokların üstündeki metinde — T136, bütün dosyanın kuralı) ve her satır
`zsh -f -i -n` ile ayrışıyor (`TestReadme_TheTailnetStepBlocksPasteIntoZsh`). Son blok bir
zincirdir: kümeden okuma ya da `kubeconfig-server` denetimi düşerse `gh` **hiç çağrılmaz** —
aynı test bloğu bash ve zsh altında sahte komutlarla **koşturarak** ölçer.

Token adım 3 ile kurulan github-deployer ServiceAccount için üretilir, cluster-admin kimliğiyle
DEĞİL. İstenen süre apiserver üst sınırıyla KIRPILABİLİR ve kubectl bunu bir uyarıyla söyler.
Kırpıldıysa süre dolmadan yenile ve takvime yaz. Yoksa deploy bir gün 401 almaya başlar.

```bash
kubectl --context hetzner-k8s-1 -n tappa create token github-deployer --duration=8760h
```

Sunucu tailnet adresidir, genel IP DEĞİL. CA kümenin kendi kök sertifikasıdır ve sır değildir.

```bash
kubectl --context hetzner-k8s-1 -n tappa get cm kube-root-ca.crt -o jsonpath='{.data.ca\.crt}' > /tmp/tappa-ca.crt
K=~/.kube/tappa-deployer.config
kubectl --kubeconfig="$K" config set-cluster tappa --server=https://10.43.0.1:443 \
  --certificate-authority=/tmp/tappa-ca.crt --embed-certs=true
kubectl --kubeconfig="$K" config set-context tappa --cluster=tappa --user=github-deployer --namespace=tappa
kubectl --kubeconfig="$K" config use-context tappa
```

Şimdi token'ı **editörle** `~/.kube/tappa-deployer.config`'e yaz: `users:` altına
`- name: github-deployer` ve `user: {token: …}`. `kubectl config set-credentials --token=…`
**kullanma** — token argv'ye ve kabuk geçmişine girer. Sonra:

Token dosyaya yazıldıktan SONRA. Tailnet içindeki bir makineden ve depo kökünden koş. Önce kümeden
okur, sonra deploy ile aynı çevrimdışı denetimi yapar. Sunucu, proxy ve TLS. İkisi de geçerse secret
yazılır. Biri düşerse zincir orada durur ve gh HİÇ çağrılmaz. base64 -w0 KULLANMA. BSD ve macOS
base64 onu reddeder, çıktı BOŞ kalır ve secret BOŞ yazılır. Aşağıdaki biçim GNU ve BSD üzerinde aynı
çıktıyı verir. Satır sonlarını tr siler.

```bash
K=~/.kube/tappa-deployer.config
kubectl --kubeconfig="$K" -n tappa get deploy tappa && \
  KUBECONFIG="$K" scripts/verify-deployment.sh kubeconfig-server 10.43.0.1 443 && \
  base64 < "$K" | tr -d '\n' | gh secret set KUBE_CONFIG --env production --repo atknatk/tappa
```

**Bugünkü durum (2026-10-19, ana oturum):** `KUBE_CONFIG` `production` ortamına bu biçimde
yazıldı — sunucu `https://10.43.0.1:443`, CA `kube-root-ca.crt`, `github-deployer` token'ı
**2027-10-19**'a kadar. 🔴 **Yenileme: 2027-10-19'dan önce** (backlog T135; değer bu belgeye
yazılmaz, yalnız tarih). Süresi dolan token ilk küme çağrısında `401` verir. (Depo düzeyindeki
eski `KUBE_CONFIG` — 2027-10-17 — ilk OPS-2 deploy'undan önce **silindi**, 2026-10-19; aşağıdaki (c).)

> ⚠️ **Bugün bu secret'ın hangi kimliği taşıdığı BU DOSYADAN doğrulanamaz** ve öyle
> yazılıyor. Eğer içindeki kubeconfig cluster-admin ise `01-rbac.yaml`'daki daraltma
> **hiçbir şeyi sınırlamaz** — cluster-admin bir kimlik Role'ü zaten aşar. Sınır
> **[kubeconfig'in kimliği doğrulanmadı]**.

**(c) GitHub `production` ortamı — YAPILDI (OPS-2, backlog T135 (4)).** Ana oturum kurdu
(2026-10-19): **Settings → Environments → `production`**, *Deployment branches* = yalnız `main`
(custom branch policy). Ortam secret'ları: `HARBOR_CI_ROBOT_NAME`, `HARBOR_CI_ROBOT_SECRET`,
`TS_OAUTH_CLIENT_ID`, `TS_OAUTH_SECRET`, `KUBE_CONFIG`. `deploy.yml`'in `deploy` işi
`environment: production` taşır ve **okuduğu her secret oradandır**; ilk adımı
(*"Preflight — the production environment's secrets must exist"*) beşini de sayar, eksik olanı
**adıyla** söyler ve hiçbir şey inşa etmeden durur (değer basmaz; `TS_OAUTH_SECRET`'ın yalnız
`tskey-client-` önekini denetler). Neden: depo düzeyindeki bir secret'ı depoya yazabilen biri bir
**dala** push'ladığı herhangi bir workflow'la okuyabilir; ortam secret'ına yalnız ortamı adlandıran
**ve** politikanın kabul ettiği dalda koşan bir iş ulaşır (`workflow_run` ve `main`'den
`workflow_dispatch` — ikisi de `main` ref'iyle koşar). Docker Hub ikilisi bu listeden **çıktı**.

✅ **Geçmiş — silme yapıldı (2026-10-19, ilk OPS-2 deploy'undan önce, ana oturum):** depo
düzeyinden **altı** secret silindi — `TS_AUTHKEY`, `KUBE_CONFIG`, `DOCKERHUB_USERNAME`,
`DOCKERHUB_TOKEN` ve 2026-10-18'de depo düzeyine de konmuş `TS_OAUTH_CLIENT_ID`/`TS_OAUTH_SECRET`
kopyaları; depo düzeyinde secret kalmadı (*"OPS-2 kapanış ölçüsü"* → *"Ölçüldü"*). Kalan,
kullanıcıda: Docker Hub token'ının ve eski auth key'in iptali.

⚠️ **Yeniden kurulum talimatı — satır tek başına koruma DEĞİL, sıra.** Depo düzeyinde bir deploy
secret'ı durdukça her workflow'a açıktır (ortam secret'ları onlara **eklenir**, onları kapatmaz;
aynı adlıda ortamınki kazanır). Depo yeniden kurulursa ya da depo düzeyinde bu adlardan biri yeniden
belirirse, `production` ortamı kurulduktan **sonra ve ilk deploy'dan önce** silinir. Deploy'u
bozmaz: iş akışı yalnız ortamın beş secret'ını okur (`TS_AUTHKEY` ve `DOCKERHUB_*`'ı hiç anmaz, aynı
adlı `KUBE_CONFIG` ve `TS_OAUTH_*`'ta ortamınki kazanır); silmek (c′)'deki kalan sınır 1'i kapatan
adımdır. Depo düzeyinde `TS_OAUTH_CLIENT_ID`/`TS_OAUTH_SECRET` kopyası varsa onlar da silinir (son
iki `delete` satırı; olmayan bir adı silmek yalnız hata basar):

```bash
gh secret delete TS_AUTHKEY --repo atknatk/tappa
gh secret delete KUBE_CONFIG --repo atknatk/tappa
gh secret delete DOCKERHUB_USERNAME --repo atknatk/tappa
gh secret delete DOCKERHUB_TOKEN --repo atknatk/tappa
gh secret delete TS_OAUTH_CLIENT_ID --repo atknatk/tappa
gh secret delete TS_OAUTH_SECRET --repo atknatk/tappa
gh secret list --repo atknatk/tappa
```

Son satır depo düzeyinde **hiç** deploy secret'ı göstermemeli. Silmeden önce Docker Hub token'ını
Docker Hub'da da **iptal et** (secret'ı silmek token'ı öldürmez); auth key'i Tailscale konsolunda
(Settings → Keys) iptal et.

**(c′) Deploy'u ne tetikler — ve neyin tetikleyemediği (backlog T140, 2026-10-19).**
`deploy.yml` yalnız iki yoldan koşar: (1) `ci`'nin **yeşil** bitmiş, **bu deponun `main`
dalına yapılmış bir push**'la başlamış koşusu; (2) Actions → `deploy` → **Run workflow**,
*Use workflow from* = **`main`** (yazma yetkisi gerekir; sınırı aşağıda). İşin `if:`'i dört
koşulun dördünü de ister (`conclusion == success`, `event == push`, `head_repository == bu
depo`, `head_branch == main`) ve ilk adım (*"Gate — this run was triggered by a push to this
repository's main"*) aynı koşulları bash'te, checkout'tan önce yeniden ölçer — **ve `if:`'ten
sıkıdır:** GitHub ifadelerinde `==` dizeleri büyük/küçük harfe **duyarsız** karşılaştırır
(`Main` adlı bir dal `if:`'i geçer), bash'in `=`'i duyarlıdır (kapı onu reddeder). Tutmazsa
koşu **kırmızı** biter (atlanmış değil), hiçbir şey build edilmez, kümeye dokunulmaz.
**Kapanan delik:** `branches: [main]` süzgeci tetikleyen koşunun **`head_branch`** adına
bakar; bir fork'un kendi `main` dalından açılan PR'ın `pull_request` koşusu da `main` adını
taşır, yeşil biterse eskiden deploy onun `head_sha`'sını (fork'un kodunu) depo secret'larıyla
build ederdi. Pin: `cmd/tappa` `TestPackaging_TheDeployRunsOnlyForAPushToThisRepositorysMain`
(tetikleyiciler, `jobs:` tam olarak `deploy`, `if:`, kapının yeri, hiçbir `run:` gövdesinde
`${{ }}` yok), `TestDeployTrigger_TheGateAcceptsOnlyAPushToMainOrAManualRunFromMain` (kapının
gövdesi koşulur) ve `TestDeployTrigger_TheCommitToDeployIsExactlyFortyLowercaseHex`
(*"Resolve the commit to deploy"* yalnız tam 40 küçük harf hex kabul eder). **Depo ayarı
(geçici, kod düzeltmesinden önce yapıldı):** Settings → Actions → *Fork pull request
workflows* = **bütün dış katkıcılar için onay** — fork PR'larının workflow'u onaysız koşmaz.
Kod düzeltmesi indikten sonra da **açık bırakılması önerilir** (kuşak + kemer; fork PR'ı
zaten secret'sız koşar, ama onay CI dakikasını ve `pull_request` yüzeyini de korur).

**Kalan sınırlar — ikisi de yalnız depoya yazma yetkisi olana açık.**

1. **Elle koşturma, düzeltmeyi taşımayan bir daldan.** `workflow_dispatch`'te GitHub
   *Use workflow from*'da seçilen **dalın kendi** `deploy.yml` kopyasını koşturur, `main`'inkini
   değil (`workflow_run`'dan farkı budur). Elle koşturma kapısı (`ref == refs/heads/main`)
   bu yüzden yalnız **bu düzeltmeyi içeren** dallarda geçerlidir; bugün (2026-10-19) uzak
   dalların **hiçbiri** onu taşımıyor. Kopyası eski ya da değiştirilmiş bir dal seçilirse
   kapı orada yoktur ve o dalın HEAD'i (`github.sha`) depo düzeyindeki secret'larla deploy
   edilir — (c)'deki risk sınıfı: yazabilen biri bir dala secret okuyan bir workflow zaten
   push'layabilir. **Kalıcı kapanış (c)'deki Environment'tır** (`production`, *Deployment
   branches* = yalnız `main`): Environment'ın dal kuralı elle koşturmada `github.ref`'e, yani
   seçilen dala bakar. Durum (c)'dedir: ortam kurulu (yalnız `main`, beş secret ortamda) ve
   iş akışı ona **bağlandı** (OPS-2, `environment: production`); depo düzeyindeki eski secret'lar
   ilk OPS-2 deploy'undan önce **silindi** (2026-10-19, altı secret; depo düzeyinde secret yok). Kapatan **satır
   değil, secret'ların yalnız Environment'ta durmasıdır**: o zaman `environment:` satırı olmayan
   eski bir kopya secret'sız koşar (secret'ı arayan ilk adımda düşer), satırı taşıyan bir kopya
   dal kuralına takılır. ✅ **Bu madde o silmeyle kapandı (2026-10-19).**
2. **`main` adlı bir etiket** [ölçülmedi]. `ci` `on: push` etiketlerde de koşar ve bir
   etiket koşusunun `head_branch`'i etiketin adıdır; `main` adında bir **etiket** push'lanırsa
   dört koşulun dördü de tutabilir. **Dal koruması yokken** bu yeni bir yetki vermez (yazma
   yetkisi zaten `main`'e push demektir); dal koruması açıldığında korumayı atlatan bir yol
   olurdu. (c)'deki Environment bunu **kapatmaz** — `workflow_run` koşusunun ref'i her zaman
   `main`'dir. **Kapanış (depo ayarı, ana oturum, 2026-10-19):** `main` adlı etiketi yasaklayan
   bir **ruleset** — aktif, istisnasız, etiket oluşturma/güncelleme/silme. Bu bir ayardır, kod
   değil. **Önerilen ikinci savunma (kodlanmadı):** kapıda, `main` fetch edildikten sonra
   `git merge-base --is-ancestor <head_sha> origin/main` — **ata denetimi, eşitlik değil**:
   eşitlik (`head_sha == main`'in ucu) deploy başladığında `main` bir sonraki push'la
   ilerlemişse meşru bir deploy'u reddeder; ata denetimi yalnız `main`'in geçmişinde olmayan
   commit'i reddeder (backlog T140). Kodlanırsa iki not: checkout derinliği 1'dir
   (`fetch-depth` varsayılanı), yani `main`'in **geçmişi** ayrıca fetch edilmelidir — yoksa
   `--is-ancestor` her meşru commit'i reddeder; ve ata denetimi `main`'in geçmişindeki **eski**
   bir commit'i (bir geri alma deploy'u) da kabul eder — bu kabul edilebilir, çünkü o commit
   zaten `main`'e girmiştir.

**(d) İlk deploy'da ölçülecekler (T134'ün kapanış ölçüsü — tarihsel; T134 2026-10-18'de kapandı,
OPS-2'nin ölçüsü *"OPS-2 kapanış ölçüsü"*nde).** `main`'deki ilk deploy koşusunun log'unda ve
Tailscale yönetim konsolunda:

- *"Join the tailnet"* yeşil. `log-mode: quiet` action'ın *"Resolved Tailscale version"* ve
  sha256 satırlarını **bastırır**; sürüm kapının özet satırında okunur
  (`tailnet: tailscale 1.94.2, …`), sha256 uyuşmazlığı ise adımı **düşürür** (action atar) —
  yani yeşil adım, indirilen arşivin `deploy.yml`'de **pinli** digest'le eşleştiği demektir
  (T135'ten önce: aynı sunucudan alınan digest'le).
- Kapı satırı `tags tag:ci` ve `cluster API reachable at 10.43.0.1:443 (attempt 1)` basar
  (`attempt 2+` rotanın geç oturduğunu söyler; bir kez kabul, sürekliyse incelenir).
- Log'da **hiçbir e-posta adresi ve status JSON'u yok** (koşu log'unda `@` araması).
- İşin sonundaki *"Post Join the tailnet"* adımında `Failed to logout` **uyarısı yok**
  (quiet kipte başarı satırı basılmaz; başarısızlık uyarı olarak basılır).
- Konsolun **Machines** listesinde koşudan sonra `github-*` cihazı **kalmıyor** — anahtar
  ephemeral değilse bile cihaz ephemeral olabilir (action `tailscaled`'i `--state=mem:` ile
  başlatır; bunun cihazı ephemeral yapıp yapmadığı **doğrulanamadı**), bu yüzden ölçü
  konsoldur, varsayım değil. Ephemeral olmayan bir cihaz kalırsa elle silinir ve anahtar
  ephemeral olarak yeniden üretilir. **Ölçüldü (T134, ilk yeşil koşu): cihaz KALDI**
  (`github-runnervm8k3b6`, çevrimdışı) — yani `--state=mem:` cihazı ephemeral **yapmadı**; OPS-2'nin
  OAuth yolu ephemeral'ı anahtarın kendisinde ister.

**(e) Tailscale istemcisini yükseltmek — `version` ve `sha256sum` BİRLİKTE (backlog T135).**
`deploy.yml`'in *"Join the tailnet"* adımı istemciyi sürümüyle ve linux/amd64 arşivinin
sha256'sıyla pinler (`KUBECTL_VERSION` / `KUBECTL_SHA256` gibi). İkisi tek bir digest'e aittir:
biri tek başına değişirse adım sha uyuşmazlığıyla **düşer** — kümeye dokunmadan, kapalı yönde.
Sürüm **kararlı** kanaldan seçilir (minor'ü çift: action tek minor'u `/unstable`'dan indirir).
Digest'i **kendin ölç**: arşivi indir, kendi makinende hashle ve yayımlanan `.sha256` ile
karşılaştır — iki satır aynı olmalı, değilse DUR. Bugünkü değer 2026-10-18'de böyle ölçüldü
(ayrıca arşivdeki `tailscale` ikilisi `go version -m` ile `tailscale.com v1.94.2`,
`GOARCH=amd64`). Runner `ubuntu-latest` = X64 = `amd64` arşivi.

```bash
V=1.94.2
U="https://pkgs.tailscale.com/stable/tailscale_${V}_amd64.tgz"
curl -fsSLo "/tmp/tailscale_${V}_amd64.tgz" "$U"
echo "published: $(curl -fsSL "$U.sha256")"
echo "measured:  $(shasum -a 256 "/tmp/tailscale_${V}_amd64.tgz" | cut -d' ' -f1)"
```

Sonra **tek commit'te**: `deploy.yml`'deki `version:` ve `sha256sum:` satırları **ve**
`cmd/tappa/deploytailnet_test.go`'daki `tailscaleVersion` / `tailscaleSHA256` sabitleri
(test digest'i değeriyle pinler; yalnız birini değiştirmek kırmızıdır). Action'ın kendi
SHA'sı da yükseltiliyorsa girdilerin adı ve davranışı yeni commit'in `action.yml`'inden ve
**koşan kodundan** — `dist/index.js` (`runs.main`); `src/main.ts` yalnız onun kaynağıdır, ikisinin
aynı davranışı yazdığı ayrıca karşılaştırılır — **yeniden okunur** (bugünkü okuma: `sha256sum` verilince `.sha256`
indirilmez, uyuşmazlık adımı atar; önbellek isabetinde denetim yoktur — `use-cache: "false"`
bu yüzden kalır).

**6. İlk deploy.** `main`'e push → `ci` yeşil → `deploy` kendiliğinden koşar.
Elle: Actions → `deploy` → Run workflow, *Use workflow from* = **`main`** (başka bir dal
seçilirse işin `if:`'i koşuyu atlar ya da — düzeltmeyi taşımayan bir dalda — o dalın HEAD'i
deploy edilir; bkz. (c′) kalan sınır 1).

**7. İlk deploy sonrası — üç doğrulama (hiçbiri isteğe bağlı değil).**

(a) TAPPA_TRUSTED_PROXIES doğru mu: mobil veriyle bir tap at, satırı oku source_ip TELEFONUN genel
adresi olmalı; 10.42.0.1 ise liste dar. (PGPASSWORD konteynerin kendi ortamından okunur -- parola
ekrana basılmaz.)

```bash
kubectl -n tappa exec statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
  psql -U tappa_owner -d tappa -Atc \
  "SELECT ip_match, source_ip FROM transactions ORDER BY created_at DESC LIMIT 1;"'
```

(b) rol ayrımı gerçekten uygulanmış mı

```bash
kubectl -n tappa exec statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
  psql -U tappa_owner -d tappa -Atc \
  "SELECT rolname, rolcanlogin, rolsuper, rolbypassrls FROM pg_roles WHERE rolname LIKE '"'"'tappa%'"'"' ORDER BY 1;"'
```

tappa_app|t|f|f · tappa_owner|t|t|t · tappa_resolver|f|f|t

(c) TLS + HSTS

```bash
curl -sS -o /dev/null -D - https://taptime.mt/healthz | \
  grep -i 'strict-transport\|^HTTP'
```

**8. `/signup`'tan ilk işletmeyi aç.** İlk deploy **boş şemadır**, seed yoktur
(kullanıcı kararı). Taptime'ın kendi yasal metinlerini (gizlilik politikası, şartlar,
künye, çerez bildirimi) **platform operatörü** yayımlar: operatör host'unda
`/operator/legal` (M10 OP-10, ADR 0020 §7) — operatör hesabı için aşağıdaki
*"Operator accounts (M10 OP-9)"* runbook'u. Müşteri panelinde bu ekran ve onun env izin
listesi yok (OP-10'da kaldırıldı); bu adım için ayarlanacak bir ConfigMap anahtarı yok.

**9. Yedek — hedefi seç, sırrı yaz, CronJob'ı uygula.**

> ⚠️ **Bu adım listenin SONUNA eklendi, araya değil.** Bu dosyada ve kartta *"operatör
> adımı 4"*, *"5. adım"*, *"adım 4.3"* gibi **numaralı** atıflar var; araya bir madde
> koymak beşini birden yanlış maddeye düşürürdü. Aynı kusur bu listede daha önce
> ölçüldü (sınır listesindeki `13.` ekleme, FAZ C 1. tur). Sıra bakımından da doğru
> yer burası: CronJob'ın mount ettiği `configmap/tappa-backup-scripts`'i `deploy.yml`
> kurar, yani **en az bir yeşil deploy'dan sonra** uygulanmalı.

🔴 **Bu adım tamamlanana kadar KÜMEDE YEDEK YOKTUR.** Ağaçta CronJob, script'ler ve
provalı geri yükleme prosedürü var; kümede `kubectl -n tappa get cronjob` **hâlâ
`No resources found`** diyor (ölçüldü 2026-08-16). Sınır **[yedek kümede HENÜZ YOK]**.

**(a) Hedefi seç — ve seçenekler ölçüldü, tahmin edilmedi.** Bu kümede off-node bir
depolama **yok**:

- `kubectl get nodes`: k8s-1.fsn1.private — tek node
- `kubectl get sc`: local-path (default), rancher.io/local-path, Delete
- `kubectl get csidrivers`: No resources found
- `kubectl get pv -o custom-columns=SC:.spec.storageClassName --no-headers | sort -u`: local-path
- `kubectl get cronjob -A`: hiçbir namespace'te yedek işi yok — emsal de yok

```bash
kubectl get nodes
kubectl get sc
kubectl get csidrivers
kubectl get pv -o custom-columns=SC:.spec.storageClassName --no-headers | sort -u
kubectl get cronjob -A
```

`local-path`, o tek node'un diskinde bir dizindir. Oraya yazılan bir dump, kopyası
olduğu veritabanıyla **aynı olayda** ölür. Bu yüzden dump `emptyDir`'de hazırlanır
(pod'la birlikte silinir) ve **tek kalıcı artefakt node'un dışına gidendir**;
gönderemezse iş **kırmızı olur** ve geride *"yedek sanılacak"* bir kopya bırakmaz.

| Seçenek | Karar | Gerekçe |
|---|---|---|
| **Cloudflare R2, AB yargı bölgesi** (`https://<hesap>.eu.r2.cloudflarestorage.com`, kova kapsamlı token) | ✅ **seçildi** (T45, orkestratör) | **Ayrı sağlayıcı**: node'un veri merkezinden de, Hetzner hesabından da bağımsız arıza alanı; AB yargı bölgesi kovası; rclone `crypt` ile şifreli. Kova/hesap ayarları orkestratörün — repo tarafı aşağıda **(b)**'deki R2 örneği |
| **AB bölgesinde S3 uyumlu nesne depolama** (ör. Hetzner Object Storage `fsn1`) | ✅ kabul edilebilir | Aynı veri merkezi ama **ayrı depolama sistemi ve ayrı arıza alanı**; §1'in AB şartına uyuyor; rclone'un `crypt` katmanıyla şifreleniyor |
| **SFTP hedefi** (Hetzner Storage Box vb.) | ✅ kabul edilebilir | Aynı rclone config'iyle; kimlik bir SSH anahtarı olur |
| **Kümedeki bir PVC** | ❌ **elendi** | `local-path` = aynı node'un diski. Sınır 1'in tarif ettiği arızada **hiçbir şey kurtarmaz** |
| **GitHub Actions'ın dump'ı çekmesi** | ❌ **elendi** | Çalışanların mesai kayıtları GitHub artefakt deposuna çıkardı — AB bölgesi dışına ve DPA'sız bir işleyene (Q23) |
| **Kümeye ikinci bir Postgres replikası** | ❌ **elendi** | Tek node'da replika **aynı diski** paylaşır; ayrıca §4.3 hatası (yanlış migration, kötü DELETE) replikaya **anında** kopyalanır — replika yedek değildir |

**(b) Sırrı yaz — iki anahtar, ve şifreleme anahtarı `TAPPA_TAG_KEK` ile AYNI YERDE
DURMAZ.**

rclone ayarini KENDI makinende, REPO DISINDA ve yalniz senin okuyabildigin bir dosyada uret (depo
PUBLIC; bu dosya R2 anahtarini ve yedek parolasini tasir):

```bash
mkdir -p -m 700 "$HOME/.config/tappa"
(umask 077 && rclone --config "$HOME/.config/tappa/backup.rclone.conf" config)
```

🔴 Kisisel ayar dosyani ($HOME/.config/rclone/rclone.conf) KOPYALAMA: icindeki her remote ve kimlik
once Secret icine, oradan her gece pod icine gider. Dosyada yalniz asagidaki iki bolum olmali.

```bash
kubectl -n tappa create secret generic tappa-backup-target \
  --from-file=rclone.conf="$HOME/.config/tappa/backup.rclone.conf" \
  --from-literal=BACKUP_REMOTE='tappa-backup:prod'
```

Dosya **emanete konup** parmak izi aşağıdaki gibi doğrulanana ve (d)'deki prova
bitene kadar bu makinede durur; sonra (d)'nin son adımı yerel kopyayı siler. Sonradan
gerekirse (geri yükleme, listeleme) emanetten **aynı yola** ve aynı izinle geri konur.
Repo içine konmaz: `.gitignore` `*.rclone.conf` ve `/restore/`'u yok sayar (T45,
`git check-ignore -v` ile ölçüldü) ama bu bir **kemer**dir, yol değil.

**R2 için `$HOME/.config/tappa/backup.rclone.conf`'un iskeleti** (yer tutucular `<…>`; değerler
**hiçbir dosyaya bu repoda** yazılmaz — §4.7, depo PUBLIC):

```ini
[r2]
type = s3
provider = Cloudflare
access_key_id = <R2 API token'ının erişim anahtarı kimliği>
secret_access_key = <R2 API token'ının gizli anahtarı>
region = auto
endpoint = https://<HESAP_ID>.eu.r2.cloudflarestorage.com
acl = private
no_check_bucket = true

[tappa-backup]
type = crypt
remote = r2:<KOVA>/tappa
password = <rclone config'in obscure ettiği parola>
password2 = <rclone config'in obscure ettiği tuz parolası>
```

Üç satır **zorunlu ve ölçüldü** (T45, 2026-10-09 — yerel bir S3 API'sine,
`rclone serve s3`, istemci tarafı `provider = Cloudflare` ile, işin kendi betiği
`scripts/pg-backup-ship.sh` uid 65532 / salt-okur kökle koşturularak; **Cloudflare'e
tek istek gitmedi**, yani R2'nin kendi davranışı aşağıda **kaynak**tır, ölçüm değil):

- 🔴 **`no_check_bucket = true`.** Kova kapsamlı bir token kova **yaratamaz** ve
  listeleyemez. Bu satır yokken iş, ilk yazmasında (`copyto` → kanarya) kovayı
  yaratmaya çalışıyor — ölçüldü: `PUT /<kova>` (**CreateBucket**) → var olan kovada
  `409`, iş **kırmızı**, *"could not write the encryption canary … Nothing has been
  uploaded."* Satır varken işin kovaya attığı her istek **nesne düzeyinde**: `PUT`
  nesne ×3 (kanarya, dump, manifest) · `GET` nesne ×1 (kanarya geri okuma) · `HEAD`
  nesne ×19 · `DELETE` nesne ×1 (kanarya) · `ListObjectsV2` ×7 — **HeadBucket,
  CreateBucket, ListBuckets: 0**. Yani token'ın **Object Read & Write** yetkisi
  yeter. Kaynak: rclone v1.71.2 belgesi, `docs/content/s3.md` → *Cloudflare R2*: *"For
  R2 tokens with the "Object Read & Write" permission, you may also need to add
  `no_check_bucket = true` for object uploads to work correctly."*; kod:
  `backend/s3/s3.go` → `makeBucket` `NoCheckBucket` iken hiçbir istek atmadan döner.
- 🔴 **Kova `crypt`'in `remote =` satırında, `BACKUP_REMOTE`'ta DEĞİL.** `crypt` kendi
  kökünün altındaki **her yol parçasını** şifreler; kök `r2:` (kovasız) olursa kova
  adı da şifrelenir. Ölçüldü: `remote = r2:` + `BACKUP_REMOTE=tappa-backup:<kova>/prod`
  → kanarya yazılamıyor (`HeadObject … input member Key must not be empty`), iş
  kırmızı, hiçbir şey yüklenmiyor, kova yaratılmıyor. Doğrusu: `remote = r2:<KOVA>/tappa`
  ve `BACKUP_REMOTE='tappa-backup:prod'`.
- **`endpoint`'teki `.eu.`** — AB yargı bölgesinde yaratılmış bir kova o uç noktadan
  erişilir. Kova ve yargı bölgesi orkestratörün; burada **ölçülmedi**.

Ölçülen başka iki şey: yüklenen nesneler `Content-Type: application/octet-stream`
ve **`Content-Encoding` taşımıyor** (crypt baytları) — rclone belgesinin *"Cloudflare
`Content-Encoding: gzip` ile yüklenen dosyaları açar"* uyarısı bu işe **değmiyor**.
Ve saklama süresi `crypt` + S3 üzerinden **çalışıyor**: rclone kaynak dosyanın
mtime'ını `X-Amz-Meta-Mtime` olarak yazıyor ve `rclone delete --min-age` ona bakıyor
— 40 günlük mtime'la yüklenen bir çift **aynı koşuda** budandı, taze çift kaldı.
R2'nin bu üst veriyi saklaması belgeye dayanır, **R2'de ölçülmedi** (aşağıdaki küme
adımı (d) onu ölçer).

🔴 **`BACKUP_REMOTE` bir yol taşımak ZORUNDA** (`remote:` yalnız başına reddedilir).
Sebep ölçülmüş: iş, saklama süresini `rclone delete --min-age` ile uyguluyor ve
çıplak bir remote'ta bu, kovadaki **başka ürünlerin** eski nesnelerini de siler.

🔴 **Şifreleme kararı ve gerekçesi.** rclone'un `crypt` remote'u hem dosya adlarını hem
içeriği şifreliyor (ölçüldü: hedef diskteki dosya adları
`uopkhhceg3gu…/e4cqqtph…`, içerik `RCLONE\0\0` sihirli baytıyla başlıyor; aynı hedef
`rclone lsl` ile açık adlarla listeleniyor). Parola `rclone.conf`'un içinde,
**`tappa-secrets`'ın içinde değil** — ve bu bilinçli: bir yedeğin var olma sebebi
*"küme gitti"* senaryosudur, ve o senaryoda `tappa-secrets`'ın yanında duran bir
yedek anahtarı da gitmiştir.

🔴 **EMANET (ESCROW) — VE İKİ SECRET'A BÖLMEK BUNU SAĞLAMAZ.** Burada bir tur boyunca
*"yedek anahtarı `tappa-secrets`'ın yanında durmasın, çünkü küme gittiğinde o da
gider"* yazıyordu ve denetim bunu çürüttü: `tappa-secrets` ile `tappa-backup-target`
**aynı namespace'te, aynı etcd'de, aynı tek node'da**. Küme giderse **ikisi de**
gider. Bölmenin gerçekten kazandırdığı tek şey **daha küçük bir okuyucu kümesi**
(dump konteyneri `POSTGRES_PASSWORD` alır ve `TAPPA_TAG_KEK`'i asla görmez; ship
konteyneri hedefi alır, ikisini de görmez). **Küme dışı emanet olmadan yedek anahtarı
kümeyle birlikte kaybolur ve yedek açılamaz.**

Emanet **`TAPPA_TAG_KEK`'ten AYRI bir zarf** olmalı: `TAPPA_TAG_KEK` kaybolursa
parktaki her plaket yeniden encode edilir ama **mesai kayıtları hâlâ okunabilir**;
yedek şifresi kaybolursa **hiçbir şey** okunamaz. Tek zarf, iki kaybı bire indirger —
yanlış yönde.

🔴 **VE EMANETİN YAPILDIĞI KANITLANIR, BEYAN EDİLMEZ.** Sınır **19** *"emanet
edilmemiş bir yedek anahtarı, yedeği olmayan bir sistemden daha kötüdür"* diyor;
doğrulanmayan bir talimat tam olarak o hâli üretir. Küme **dışındaki** kopyayla,
kümedeki kopyanın aynı olduğunu **değerini göstermeden** karşılaştır:

(1) kümedeki kopyanın parmak izi — değer ekrana BASILMAZ, yalnız sha256 öneki

```bash
kubectl -n tappa get secret tappa-backup-target -o jsonpath='{.data.rclone\.conf}' \
  | base64 -d | sha256sum | cut -c1-16
```

(2) emanetteki kopyanın parmak izi (kasadan/parola yöneticisinden indirdiğin dosya)

```bash
sha256sum /path/to/escrowed-rclone.conf | cut -c1-16
```

🔴 İKİSİ EŞİT DEĞİLSE EMANET YOK DEMEKTİR. Eşitse bu adım tamamdır.

⚠️ Bu iki komut `tappa-backup-target`'ı okur, **`tappa-secrets`'ı değil** — bu dosya
`tappa-secrets` üzerinde `jsonpath` denemeyi §4.7 gereği yasaklıyor. Ve çıktı bir
**hash önekidir**, sırrın kendisi değil.

**(c) CronJob'ı uygula ve ilk koşuyu ELLE tetikle** (gece 02:30'u bekleme — çalışmayan
bir yedeği ertesi sabah öğrenmek, hiç yedek olmamasından farksızdır):

- `kubectl -n tappa logs job/tappa-backup-first --tail=20`: ship konteyneri

```bash
kubectl apply -f deploy/k8s/50-backup.yaml
kubectl -n tappa create job tappa-backup-first --from=cronjob/tappa-backup
kubectl -n tappa wait --for=condition=complete job/tappa-backup-first --timeout=3600s
kubectl -n tappa logs job/tappa-backup-first -c dump-and-verify
kubectl -n tappa logs job/tappa-backup-first --tail=20
kubectl -n tappa delete job tappa-backup-first
```

`dump-and-verify` logunun son satırları şu şekli taşımalı (ölçülmüş gerçek çıktı,
goose **34** şemasında 6,8 milyon satırlık geliştirme veritabanından — T45,
2026-10-09; 2026-08-16'daki goose 20 ölçümü 19 tablo / 18 politika / 18 `FORCE` /
105 GRANT diyordu):

```
pg-backup: connected as tappa_owner (rolsuper=true rolbypassrls=true), row_security stays off
pg-backup: live schema: 24 tables, 20 policies, 23 tables with FORCE RLS, goose 34
pg-backup: live inventory: 35 functions (21 SECURITY DEFINER), 5 roles own or hold something in public
pg-backup: verified: 24/24 tables present, 6779780 rows in the dump (6779780 live now), 20 policies, 23 ENABLE / 23 FORCE RLS, 240 GRANTs, 35/35 function owners
pg-backup: backup instant (restore rewinds to here): 2026-10-09T11:58:20Z
```

`24 tables` ama `23 FORCE`: fark `goose_db_version`'dır (goose'un defteri, RLS'siz) ve
doğrudur — `pg-restore-verify.sh` tablo sayısını ve `FORCE` sayısını **ayrı ayrı**
manifest'le karşılaştırır.

⚠️ **Taze bir kurulumda satır sayısı `0` olur ve bu DOĞRUDUR** — hiçbir kontrol
*"sıfırdan fazla satır"* istemiyor, çünkü sağlıklı bir yeni kurulum **boştur**. Kırmızı
olan şey *"canlıda satır var, dump'ta yok"*tur.

**Kova kilidi — orkestratör kararı (T45).** İlk yeşil koşudan sonra R2'de bir kova
kilidi (bucket lock) kurulur: **14 gün**, ve **yalnız `tappa/<enc(prod)>/` önekine**.
Budama 30 günde olduğu için kilitle çakışmaz; kanarya (crypt kökünde, `tappa/<opak ad>`)
ve (d)'nin `prune-probe`'u bu önekin **dışında** kaldığı için silinebilir kalır. Önekin
şifreli adı config'ten çıkarılır (bir dizin adının şifreli hâlidir, sır değildir):

```bash
rclone --config "$HOME/.config/tappa/backup.rclone.conf" cryptdecode --reverse tappa-backup: prod
```

Çıktının ikinci sütunu `<enc(prod)>`'dur; kovadaki önek `tappa/<o değer>/`. Kilidi
orkestratör kurar; bu repo kilidin varlığını ölçemez.

**(d) Hedeften GERİ AL ve atılabilir bir Postgres'e geri yükle — kendi makinende,
kümeye dokunmadan.** Bir yedek, geri alınıp doğrulanana kadar bir iddiadır. İlk yeşil
koşudan sonra, (b)'deki config ile, **bu commit'in checkout'unun kökünde**, beş blok
**sırayla ve aynı kabukta** (her blok bir öncekinin değişkenlerini kullanır). Bir blok
beklenen sonucu vermezse **sonrakine geçme**: (d4) boş ya da yarım bir veritabanını
doğrular, (d5) konteyneri siler. Döküm repo içine değil, `mktemp -d`'nin verdiği
dizine iner.

(d1) Geri al ve bütünlüğü doğrula. `STAMP` en yeni damgayı seçer; başka bir damga
isteniyorsa listeden elle yazılır. (Bloklarda bilerek **hiç yorum yok**: zsh'in
etkileşimli kabuğunda `interactive_comments` kapalıdır ve yapıştırılan bir `#` satırı
komut olarak koşar.)

```bash
CONF="$HOME/.config/tappa/backup.rclone.conf"
RESTORE=$(mktemp -d)
rclone --config "$CONF" lsf --dirs-only tappa-backup:prod
STAMP=$(rclone --config "$CONF" lsf --dirs-only tappa-backup:prod | sort | tail -n 1 | tr -d /)
rclone --config "$CONF" copy "tappa-backup:prod/$STAMP" "$RESTORE/"
want=$(awk '/^sha256 /{print $2}' "$RESTORE/tappa-$STAMP.manifest")
have=$(shasum -a 256 "$RESTORE/tappa-$STAMP.sql.gz" | awk '{print $1}')
if [ "$want" = "$have" ]; then echo "sha256 OK $STAMP"; else echo "sha256 MISMATCH: DUR"; fi
```

(d2) Atılabilir hedef — `deploy/k8s/10-postgres.yaml`'ın kurduğu gibi (aynı imaj, aynı
iki init dosyası, her yerde scram). Init dosyaları **`docker cp`** ile girer, bind-mount
ile değil: Docker Desktop'ta bind-mount edilmiş 0644 bir `.sh` entrypoint'in `[ -x ]`
sınamasını geçip *"bad interpreter: Permission denied"* ile düştü (ölçüldü); ConfigMap
gerçek bir 0644 dosyadır ve entrypoint onu **source** eder. Parolalar yalnız bu prova
için, kabukta doğar:

```bash
POSTGRES_PASSWORD=$(openssl rand -hex 24); TAPPA_APP_PASSWORD=$(openssl rand -hex 24)
export POSTGRES_PASSWORD TAPPA_APP_PASSWORD
docker network create tappa-drill
docker create --rm --name tappa-drill-db --network tappa-drill \
  -e POSTGRES_USER=tappa_owner -e POSTGRES_DB=tappa -e POSTGRES_PASSWORD -e TAPPA_APP_PASSWORD \
  -e POSTGRES_INITDB_ARGS="--auth-local=scram-sha-256 --auth-host=scram-sha-256" postgres:17-alpine
docker cp scripts/db-init/01-roles.sql tappa-drill-db:/docker-entrypoint-initdb.d/01-roles.sql
docker cp deploy/k8s/postgres-init/02-app-password.sh tappa-drill-db:/docker-entrypoint-initdb.d/02-app-password.sh
docker start tappa-drill-db
until docker exec tappa-drill-db pg_isready -q -h 127.0.0.1 -U tappa_owner -d tappa; do sleep 1; done
```

(d3) B YOLU'nun **B2**'si, **B3**'ün geri yüklemesi ve **B3**'ün daraltması — aynı
SQL, `kubectl … exec -i statefulset/tappa-postgres --` yerine `docker exec -i
tappa-drill-db` (docker'ın `exec`'i `--` **almaz**). Geri yükleme `exit 0` vermezse dur:

```bash
docker exec -i tappa-drill-db sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE USAGE, SELECT ON SEQUENCES FROM tappa_app;
SQL
gzip -dc "$RESTORE/tappa-$STAMP.sql.gz" | docker exec -i tappa-drill-db \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -U tappa_owner -d tappa'
echo "restore exit: $?"
docker exec -i tappa-drill-db sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE UPDATE, DELETE ON TABLES FROM tappa_app;
SQL
```

(d3b) **B4**'ün ilk bloğu — punch akışının sayaçları. Yalnız goose 48 ve sonrasının dökümünde: daha
eskisinde tablo yoktur, bu blok atlanır ve araç *"no punch feed"* deyip geçer:

```bash
docker exec -i tappa-drill-db sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
UPDATE public.feed_counters SET last_seq = last_seq + 1000000;
SQL
```

(d4) **B4** — doğrulama aracı, `tappa_app` olarak TCP üzerinden. `PGPASSWORD` docker'a
**adıyla** verilir (`-e PGPASSWORD`, değersiz): değer argv'ye değil ortamdan geçer.
`PASS` ve `exit 0` görmeden prova başarılı sayılmaz:

```bash
PGPASSWORD="$POSTGRES_PASSWORD" docker run --rm --network tappa-drill \
  -v "$RESTORE:/restore:ro" -v "$PWD/scripts:/scripts:ro" \
  -e PGHOST=tappa-drill-db -e PGPASSWORD -e TAPPA_APP_PASSWORD \
  postgres:17-alpine sh /scripts/pg-restore-verify.sh "/restore/tappa-$STAMP.sql.gz" "/restore/tappa-$STAMP.manifest"
echo "verify exit: $?"
```

⚠️ **(isteğe bağlı) Saklama süresinin hedefte çalıştığını ölçmek — R2'de ölçülmedi.**
rclone her nesneye kaynak dosyanın mtime'ını `X-Amz-Meta-Mtime` olarak yazar ve
`--min-age` ona bakar (yerel S3 API'sinde ölçüldü). Hedef bu üst veriyi saklamazsa
rclone yükleme anına düşer — dump'ın mtime'ı ile yükleme anı dakikalar ayrı olduğu için
**bu da doğru sonuç verir**; ölçüm yalnız hangisinin geçerli olduğunu söyler. `lsl`
satırında **2025-01-01** görünüyorsa üst veri saklanıyor; son `lsf` boş dönmeli:

```bash
PROBE=$(mktemp)
touch -t 202501010000 "$PROBE"
rclone --config "$CONF" copyto "$PROBE" tappa-backup:prune-probe/prune-probe.txt
rclone --config "$CONF" lsl tappa-backup:prune-probe
rclone --config "$CONF" delete tappa-backup:prune-probe --min-age 30d
rclone --config "$CONF" lsf tappa-backup:prune-probe
rm -f "$PROBE"
```

(d5) Temizlik — konteyner, ağ, parolalar ve **döküm dizini** (çalışanların mesai kaydı):

```bash
docker stop tappa-drill-db
docker network rm tappa-drill
unset POSTGRES_PASSWORD TAPPA_APP_PASSWORD
rm -rf "$RESTORE"
```

(d6) **Config'in yerel kopyası — YALNIZ (b)'deki iki sha256 öneki EŞİTSE** (emanet
doğrulandıysa). Eşit değilse **silme**: küme dışındaki tek kopya budur ve onsuz
*"küme gitti"* senaryosunda yedek açılamaz. *(T45 kapanışında d5'ten ayrıldı — komut
aynı; koşulsuz silme denetimde DÜŞÜK bulguydu.)*

```bash
rm -f "$CONF"
```

**T45 2. turda bu bloklar (d6 o gün d5'in son satırıydı), yazıldığı gibi ve bu sırayla koşuldu** — geliştirme
veritabanının 6,8 M satırlık yedeğiyle, R2 yerine yerel bir S3 API'sine
(`rclone serve s3`) karşı, `rclone` komutu aynı imajı (`rclone/rclone:1.71`) çağıran
bir kabuk fonksiyonuna sarılarak ve `HOME` atılabilir bir dizine çevrilerek. **İki kez:**
bash'te, ve etkileşimli bir zsh'e (`zsh -f -i`, `interactive_comments` kapalı)
stdin'den yapıştırılır gibi verilerek. İkisinde de: `sha256 OK`, `restore exit: 0`,
`pg-restore-verify: PASS`, `verify exit: 0`, `prune-probe` `lsl`'de 2025-01-01 ve
budamadan sonra boş; zsh'te `command not found` **0**; sonunda kalan konteyner, ağ,
indirilmiş döküm ve yerel config **0**, checkout'ta yeni dosya **0**. Ayrıntılar
*"Yedek ve geri yükleme"* → *"Provanın kendisi"*.

**10. Dış uyarı — healthchecks.io ölü adam anahtarı (T116; `Q28 (a)`'nın bir parçası).**

> ⚠️ **Bu adım da listenin SONUNA eklendi** — 9. adımın notundaki gerekçe aynen geçerli:
> numaralı atıflar (*"adım 9(b)"*, *"5. adım"*) araya konan bir maddeyle kayar. Sıra
> bakımından da doğru yer burası: yedek sinyalini gönderen `scripts/pg-backup-ship.sh`
> `configmap/tappa-backup-scripts`'e ancak **bu değişikliği içeren bir deploy**'la girer.

**Ne ve neden.** *"M8-03 — UYARI KURALLARI"*nın kuralları kümenin **kendi log'undan**
hesaplanır; ölü bir node log yazmaz, yani hiçbiri *"küme gitti"* diyemez. healthchecks.io
bunun tersini yapar: belli aralıklarla *"yaşıyorum"* denmesini bekler ve **söylenmesi
kesilince** bildirim atar — Telegram **ve** e-posta (kullanıcı kararı 2026-10-09: AB'de
barındırılan dış servis, ücretsiz plan; ⚠️ e-posta 2026-10-11'e kadar **hiçbir** kontrole
bağlı değildi — tuzak 1, (f)). Kontrolün sinyal adresine bir `GET` *"yaşıyor"*, adresin
sonuna `/fail` *"hemen uyar"* demektir. Üç kontrol kurulu — üçüncüsü M8-03 log kurallarının
teslimatıdır (T118) ve sinyali adresten değil **gövdedeki kelimeden** okur:

| Kontrol | Takvim / tolerans | Sinyali gönderen | *"Yaşıyor"* | `/fail` — hemen uyarı | Sinyal yok — tolerans sonunda uyarı |
|---|---|---|---|---|---|
| `tappa-uptime` | her 5 dk / 5 dk | `k8s/55-heartbeat.yaml` (CronJob `tappa-heartbeat`) | halka açık `$TAPPA_BASE_URL/healthz` → 200 | `/healthz` 200 değil: TLS, DNS, bağlantı, zaman aşımı, 5xx — ve hazırlığı düşen pod Service'ten çıktığı için veritabanı arızası da (`replicas: 1`) | node/küme yok, CronJob askıda, Secret yok, imaj çekilemiyor, sinyal teslim edilemedi |
| `tappa-backup` | cron `30 2 * * *` Europe/Malta / 1 saat | `k8s/50-backup.yaml`'ın `ship` konteyneri (`scripts/pg-backup-ship.sh`'ın EXIT tuzağı) | dump R2'ye gönderildi **ve** hedefte doğrulandı | `ship` kırmızı bitti (hedef, şifreleme kanıtı, yükleme, doğrulama, budama) | `dump-and-verify` düştü (`ship` o zaman **hiç başlamaz** — bilerek), CronJob koşmadı, sinyal teslim edilemedi |
| `tappa-signals` | 365 gün / 1 saat — yani fiilen **ölü adam anahtarı DEĞİL** | SigNoz'un webhook kanalı `tappa-healthchecks` (M8-03 kurallarının hepsi; `POST`) | gövdede `resolved` — bir kural kapandı | gövdede `firing` — bir kural ateşlendi | 365 gün hiç bildirim yok. SigNoz'un **kendi** ölümünü bu kontrol **görmez** (sınır 25) |

🔴 **Sinyal adresi bir SIRdır:** bilen biri *"yaşıyor"* gönderip uyarıyı susturabilir.
`secret/tappa-alert-pings`'te durur (`UPTIME_PING_URL`, `BACKUP_PING_URL` — tam
`https://hc-ping.com/<uuid>` adresleri), her pod **yalnız kendi anahtarını** alır ve adres
hiçbir log satırına düşmez: iki betik de HTTP istemcisinin çıktısını basmaz, log'da yalnız
karar ve HTTP sınıfı vardır. Heartbeat adresi curl'e **stdin**'den verir (argv'de yok,
ölçüldü); yedeğinki argv'dedir (sınır 33 (c)).

🔴 **Yedek, alarmına BAĞIMLI DEĞİLDİR; heartbeat ise bağımlıdır — ikisi bilerek ters.**
`50-backup.yaml`'da anahtar `optional: true`: Secret yoksa, adres https değilse ya da
healthchecks.io cevap vermezse yedek **yine alınır ve iş yeşil biter**, log'a tek satır
düşer (`TestBackupShip_TheSignalNeverChangesTheOutcome` her durumu sinyalli ve sinyalsiz
koşup iki çıkış kodunu karşılaştırır). `55-heartbeat.yaml`'da anahtar **zorunlu**: Secret
yoksa pod açılmaz, sinyal gitmez, servis uyarır — kendi sinyalini sessizce atlayan bir
heartbeat hiç uyaramayan tek yapılandırma olurdu.

**(a) Secret — değer ekrana, argv'ye ve kabuk geçmişine çıkmadan.** Orkestratör
2026-10-09'da yarattı; bu blok taze bir küme içindir. Adresler healthchecks.io panelinde,
her kontrolün ping adresi alanındadır. `printf` ve `read` kabuğun kendi komutlarıdır,
yani değerler bir sürecin argv'sine girmez. 🔴 `apply` DEĞİL `create`: `apply` nesnenin
tamamını `last-applied-configuration` açıklamasına **düz metin** yazar.

```bash
umask 077
printf 'tappa-uptime ping URL: '; read -rs UPTIME_URL; echo
printf 'tappa-backup ping URL: '; read -rs BACKUP_URL; echo
printf '{"apiVersion":"v1","kind":"Secret","metadata":{"name":"tappa-alert-pings","namespace":"tappa"},"type":"Opaque","stringData":{"UPTIME_PING_URL":"%s","BACKUP_PING_URL":"%s"}}' "$UPTIME_URL" "$BACKUP_URL" \
  | kubectl --context hetzner-k8s-1 -n tappa create -f -
unset UPTIME_URL BACKUP_URL
```

Doğrula — yalnız ADLAR, BOYUTLAR ve açıklama ANAHTARLARI, değer basılmaz:

```bash
kubectl --context hetzner-k8s-1 -n tappa get secret tappa-alert-pings -o go-template='{{range $k, $v := .data}}{{$k}} {{len $v}}{{"\n"}}{{end}}{{range $k, $v := .metadata.annotations}}annotation {{$k}}{{"\n"}}{{end}}'
```

Beklenen: iki satır, `BACKUP_PING_URL 76` ve `UPTIME_PING_URL 76` — 76, 56 baytın base64
uzunluğudur (`https://hc-ping.com/` 20 + UUID 36; healthchecks.io'nun *slug* biçimli adresi
başka bir uzunluk verir). Fazlası değere bir satır sonu girdiğini gösterir: iki betik de
boşluk taşıyan bir adresi **reddeder** (heartbeat kırmızı, yedek tek satır uyarı). Bir
`annotation kubectl.kubernetes.io/last-applied-configuration` satırı görünürse Secret
`apply` ile yaratılmış ve adresler o açıklamada düz metin duruyor:
`kubectl --context hetzner-k8s-1 -n tappa annotate secret tappa-alert-pings kubectl.kubernetes.io/last-applied-configuration-`.

**Bir adres sızdıysa** healthchecks.io'da o kontrolün adresini yenile (ya da kontrolü
yeniden yarat) ve yalnız o anahtarı yamala — değer yine stdin'den:

```bash
printf 'yeni ping URL: '; read -rs NEW_URL; echo
printf '{"stringData":{"UPTIME_PING_URL":"%s"}}' "$NEW_URL" | kubectl --context hetzner-k8s-1 -n tappa patch secret tappa-alert-pings --type merge --patch-file /dev/stdin
unset NEW_URL
```

(yedeğinki için anahtar `BACKUP_PING_URL`). Rollout gerekmez: her iki CronJob da her
koşuda yeni bir pod açar ve Secret'ı o an okur.

**(b) CronJob'ları uygula — bu değişiklik `main`'e birleşip deploy yeşil koştuktan SONRA.**

```bash
kubectl --context hetzner-k8s-1 apply -f deploy/k8s/55-heartbeat.yaml
kubectl --context hetzner-k8s-1 apply -f deploy/k8s/50-backup.yaml
```

Ters sıra zararsızdır ama sessiz değildir: eski `pg-backup-ship.sh` `BACKUP_PING_URL`'yi
okumaz, yedek yeşil biter, `tappa-backup` kontrolü sinyal alamadığı için **uyarır**.

**(c) İlk sinyalleri ELLE tetikle** — 5 dakikayı ve 02:30'u bekleme:

```bash
kubectl --context hetzner-k8s-1 -n tappa create job tappa-heartbeat-first --from=cronjob/tappa-heartbeat
kubectl --context hetzner-k8s-1 -n tappa wait --for=condition=complete job/tappa-heartbeat-first --timeout=120s
kubectl --context hetzner-k8s-1 -n tappa logs job/tappa-heartbeat-first
kubectl --context hetzner-k8s-1 -n tappa delete job tappa-heartbeat-first
```

Beklenen log, iki satır — ve healthchecks.io panelinde `tappa-uptime` **up**:

```
tappa-heartbeat: probe https://taptime.mt/healthz: up (HTTP 200)
tappa-heartbeat: signal sent (HTTP 2xx)
```

✅ **Pod'dan node'un kendi genel adresine (hairpin) TLS bağlantısı bu kümede ÖLÇÜLDÜ
(orkestratör, 2026-10-09):** `tappa` namespace'inde, `restricted` PodSecurity altında koşan
bir `curlimages/curl:8.22.0` pod'undan `https://taptime.mt/healthz` → **200** (uzak adres
`144.76.158.60`, 0,028 sn), `https://ops.taptime.mt/operator/login` → 200,
`https://hc-ping.com/` → 301. Yani `--resolve`/`--connect-to` gerekmiyor. Bu ölçüm bir
**an**dır: ilk koşuda yine de `DOWN (connect…)`/`(timeout…)` görür ve dışarıdan
`https://taptime.mt/healthz` 200 dönüyorsa önce hairpin'in değişip değişmediğine bak.

```bash
kubectl --context hetzner-k8s-1 -n tappa create job tappa-backup-signal-first --from=cronjob/tappa-backup
kubectl --context hetzner-k8s-1 -n tappa wait --for=condition=complete job/tappa-backup-signal-first --timeout=3600s
kubectl --context hetzner-k8s-1 -n tappa logs job/tappa-backup-signal-first -c ship --tail=2
kubectl --context hetzner-k8s-1 -n tappa delete job tappa-backup-signal-first
```

Beklenen son iki satır: `pg-backup-ship: done: N backup(s) retained at the destination` ve
`pg-backup-ship: alert signal sent (success, HTTP 2xx)`; panelde `tappa-backup` **up**. Bu
tam bir yedek koşusudur — hedefe bir kopya daha gider (budama yaşa göre; kova kilidi
14 gün). Son olarak e-posta kanalının kendisini healthchecks.io panelindeki entegrasyonun
test bildirimiyle sına — bu repodan ölçülemez. ⚠️ **Test bildiriminin gelmesi kontrolün o
kanala BAĞLI olduğunu kanıtlamaz** (tuzak 1, (f)): her kontrolün sayfasında Telegram **ve**
e-postanın ikisinin de açık olduğunu ayrıca gör.

**(d) Uyarı geldiğinde — belirti → ilk bakılacak yer.** Son işler:
`kubectl --context hetzner-k8s-1 -n tappa get job -l app.kubernetes.io/component=heartbeat --sort-by=.metadata.creationTimestamp`
(yedek için `component=backup`).

| Bildirim (Telegram / e-posta) | Anlamı | İlk bakılacak yer |
|---|---|---|
| `tappa-uptime` **down**, hemen | heartbeat koştu ve `/fail` gönderdi: `/healthz` 200 değildi | son işin log'undaki `DOWN (…)` parantezi: `HTTP 503` → hazırlık/veritabanı (*"Olay müdahalesi"*, M8-03 6. sinyal) · `tls` → sertifika (`get certificate`) · `dns` → adım 1 · `connect`/`timeout` → ingress, node ya da hairpin |
| `tappa-uptime` **down**, tolerans dolunca | hiç sinyal gelmedi | önce dışarıdan `https://taptime.mt/healthz`; sonra `get nodes`, `get cronjob tappa-heartbeat` (askıda mı) ve son işin kendisi: `describe job <iş>` — `DeadlineExceeded` = pod 60 sn'yi aştı; ⚠️ Job denetleyicisi o anda **pod'u siler**, yani `get pod -l app.kubernetes.io/component=heartbeat` çoğu zaman **boş** döner. Sebep pod olaylarındadır (~1 saat kalır): `get events --field-selector involvedObject.kind=Pod --sort-by=.lastTimestamp` — `CreateContainerConfigError` = Secret ya da ConfigMap yok · `ErrImagePull`/`ImagePullBackOff` = imaj ya da Docker Hub bütçesi (sınır 12). Pod kaldıysa log'unda `signal NOT delivered` = healthchecks.io'ya ulaşılamıyor |
| `tappa-backup` **down**, hemen | `ship` kırmızı bitti ve `/fail` gönderdi | `logs job/<iş> -c ship` → `FAILED:` satırı; *"Yedek ve geri yükleme"* |
| `tappa-backup` **down**, 03:30'dan sonra (Malta) | sinyal yok | `get job -l app.kubernetes.io/component=backup`; `logs job/<iş> -c dump-and-verify`; `ship` log'unda `alert signal skipped` ya da `NOT delivered` |
| `tappa-signals` **down** | SigNoz'da M8-03 kurallarından **en az biri** ateşlendi | SigNoz arayüzü → *Alerts* — ateşteki kuralın adı `tappa <n>: …`'dir ve `<n>` *"M8-03 — UYARI KURALLARI"* tablosunun satırıdır; o satırın *"İlk bakılacak yer"* sütunu |
| `tappa-signals` **up** | bir kural kapandı | ⚠️ **ötekilerin de kapandığı anlamına gelmez** — tek kontrol bütün kuralları taşır (sınır 25); SigNoz → *Alerts*'te ateşte kural kalmadığını gör |

**(e) Sınırlar — sayılı, ayrıntısı sınır 33'te (`tappa-signals`'ınki sınır 25'te):** tek
sağlayıcı ve tek hesap (iki kanal: Telegram + e-posta); ücretsiz planın kotası/SLA'sı
ölçülmedi; yoklama küme **içinden** başlar (node dışındaki bir ağ arızası görünmez;
hairpin'in **çalıştığı** 2026-10-09'da ölçüldü); yedek sinyalinin adresi kısa bir an argv'de.

**(f) `tappa-signals` — M8-03'ün log kurallarının teslimatı (T118, 2026-10-11; 8. kural T122).**
Orkestratör kümede kurdu; bu alt adım bir **sonraki** kurulum içindir. Zincir:

> SigNoz kuralı (kaynağı `deploy/observability/signoz-alert-rules.json`) → SigNoz webhook
> kanalı `tappa-healthchecks` → healthchecks.io kontrolü `tappa-signals` → Telegram + e-posta.

SigNoz'un kendi e-posta özelliği **kapalı**; tek teslim yolu bu zincirdir. Uçtan uca kanıt
(orkestratör, 2026-10-11): SigNoz'un kanal test uyarısı, bildirim kanalı bağlanmamış bir
sonda kontrolünde `fail`/`down` üretti (sonda sonra silindi). Kuralların ne olduğu, neyi
basitleştirdikleri ve neyin onları susturduğu *"M8-03 — UYARI KURALLARI"* bölümünün başındaki
kutudadır.

`tappa-signals`'ın ayarları — dördü de **zorunlu**, biri eksikse zincir sessizce kırılır:

| Ayar | Değer | Eksikse |
|---|---|---|
| `filter_http_body` | `true` | gövde okunmaz; her istek *"yaşıyor"* sayılır, uyarı **hiç** gelmez (tuzak 2) |
| `failure_kw` / `success_kw` | `firing` / `resolved` | SigNoz'un gövdesindeki durum kelimeleri; kural açıklamaları bu iki kelimeyi **taşıyamaz** (`TestSignozRules_CarryNoAddressAndNoDeliveryKeyword`) |
| `channels` | `*` | kontrol hiçbir kanala bağlanmaz: `down` olur ve **kimseye söylemez** (tuzak 1) |
| `timeout` / `grace` | 365 gün (API'nin üst sınırı, 31 536 000 sn) / 1 saat | bu kontrol bir ölü adam anahtarı **değildir**; kısa bir tolerans, sessiz geçen her gün için yanlış uyarı demek olurdu |

Kontrol **yeni** (`new`) durumdayken uyarmaz — ilk `firing`'e kadar sessizdir. Yani *"hiç
bildirim gelmedi"* zincirin çalıştığını **kanıtlamaz**; tek kanıt yukarıdaki uçtan uca
denemedir ve yeniden kurulumda, orkestratörün yaptığı gibi **gerçek kontrolü çaldırmadan**
tekrarlanır: healthchecks.io'da aynı ayarlarla ama **kanalsız** geçici bir sonda kontrolü
kur; aşağıdaki (2)'yi koşarken adres sorusuna önce **sondanın** ping adresini ver; SigNoz
arayüzünde kanalın düzenleme formundaki *Test* ile sondada `down` gör; sonra aynı formda
adresi `tappa-signals`'ınkiyle değiştir ve sondayı sil.

🔴 **Sinyal adresi bir SIRdır** — bilen biri `resolved` göndererek açık bir uyarıyı kapatır.
Infisical `/tappa-alerts/TAPPA_HC_SIGNALS_PING_URL`'de ve SigNoz'un kanal ayarında durur;
repoda **yoktur** (`deploy/observability/` altında `hc-ping` ve `://` geçemez — aynı test).
Sızarsa: healthchecks.io'da adresi yenile, Infisical'ı ve SigNoz kanalını güncelle.

⚠️ **İKİ TUZAK — ikisi de ölçüldü (orkestratör, 2026-10-11):**

1. **API ile kurulan bir kontrol varsayılan olarak HİÇBİR bildirim kanalına bağlanmaz.**
   `"channels": "*"` açıkça verilmelidir (API belgesine göre `*` o an **var olan** bütün
   entegrasyonları bağlar — sonradan eklenen bir kanal her kontrole ayrıca bağlanır).
   Ölçüldü: `tappa-uptime` ve `tappa-backup` yalnız Telegram'a bağlıydı, e-posta
   entegrasyonu **hiçbir** kontrole bağlı değildi — yani bu adımın 2026-10-09 metnindeki
   *"e-postayla"* iki gün boyunca doğru değildi. Orkestratör üçünü de `*` yaptı.
2. **HTTP gövdesinin anahtar kelime filtresi `filter_http_body`'dir; `filter_body` E-POSTA
   gövdesidir** (healthchecks.io'ya e-postayla gelen sinyaller için). Ölçüldü: `filter_body`
   ile kurulan kontrol `firing` taşıyan isteği **`success`** saydı — her uyarı
   *"yaşıyor"* olarak okunuyordu.

**Yeniden kurulum.** ⚠️ Bu iki blok bu repodan **koşulmadı** (yapıcı SigNoz'a ve
healthchecks.io'ya istek atmadı); uç noktalar ve gövde biçimleri orkestratörün 2026-10-11
kurulumundakilerdir. Değerler `read -rs` ile okunur ve curl'e **yapılandırma dosyası**
(`-K`, `umask 077`) ya da **stdin** üzerinden verilir — hiçbiri bir sürecin argv'sine ya da
ekrana düşmez. `printf` ve `read` kabuğun kendi komutlarıdır.

(1) healthchecks.io kontrolü — panelden de kurulabilir; API'yle kurulursa gövde budur.
`unique` var olan bir kontrolü **güncellemez**, yalnız ikincisini yaratmayı engeller: kontrol
zaten varsa ayarlarını panelde yukarıdaki tabloyla karşılaştır. Yanıttan yalnız sırsız
alanlar basılır (`ping_url` basılmaz).

```bash
umask 077
HC_CFG=$(mktemp)
printf 'healthchecks.io API key (read-write): '; read -rs HC_KEY; echo
printf 'header = "X-Api-Key: %s"\nheader = "Content-Type: application/json"\n' "$HC_KEY" > "$HC_CFG"
unset HC_KEY
printf '%s' '{"name":"tappa-signals","timeout":31536000,"grace":3600,"filter_http_body":true,"failure_kw":"firing","success_kw":"resolved","channels":"*","unique":["name"]}' \
  | curl -fsS -K "$HC_CFG" --data-binary @- https://healthchecks.io/api/v3/checks/ \
  | jq '{name, status, timeout, grace, failure_kw, success_kw, channels}'
rm -f "$HC_CFG"
```

Beklenen: `name` `tappa-signals`, `status` `new`, `channels` **boş değil**. Sonra kontrolün
ping adresini panelden Infisical `/tappa-alerts/TAPPA_HC_SIGNALS_PING_URL`'e yaz.

(2) SigNoz — oturum, kanal, kurallar. Sıra önemlidir: kurallar kanalı **adıyla** anar,
yani kanal önce kurulur. Kurallar ikinci kez gönderilirse aynı adla **ikinci** bir kopya
doğar — taze olmayan bir SigNoz'da önce *Alerts*'te var olanlara bak.

```bash
umask 077
SIGNOZ=https://signoz.everva.com.tr
SZ_CFG=$(mktemp)
printf 'SigNoz admin e-mail: '; read -rs SZ_EMAIL; echo
printf 'SigNoz admin password: '; read -rs SZ_PASS; echo
SZ_ORG=$(printf 'url = "%s/api/v2/sessions/context?email=%s"\n' "$SIGNOZ" "$(printf '%s' "$SZ_EMAIL" | jq -sRr @uri)" | curl -fsS -K - | jq -r '.data.orgs[0].id')
[ -n "$SZ_ORG" ] && [ "$SZ_ORG" != null ] && echo 'org ok'
printf 'url = "%s/api/v2/sessions/email_password"\nheader = "Content-Type: application/json"\n' "$SIGNOZ" > "$SZ_CFG"
SZ_TOKEN=$(printf '%s\n%s\n%s\n' "$SZ_EMAIL" "$SZ_PASS" "$SZ_ORG" | jq -Rn '[inputs] | {email: .[0], password: .[1], orgId: .[2]}' | curl -fsS -K "$SZ_CFG" --data-binary @- | jq -r '.data.accessToken')
unset SZ_EMAIL SZ_PASS
[ ${#SZ_TOKEN} -gt 20 ] && [ "$SZ_TOKEN" != null ] && echo 'token ok'
printf 'header = "Authorization: Bearer %s"\nheader = "Content-Type: application/json"\n' "$SZ_TOKEN" > "$SZ_CFG"
unset SZ_TOKEN
printf 'tappa-signals ping URL: '; read -rs HC_SIGNALS_URL; echo
printf '%s' "$HC_SIGNALS_URL" | jq -Rs '{name: "tappa-healthchecks", webhook_configs: [{send_resolved: true, url: .}]}' \
  | curl -sS -K "$SZ_CFG" --data-binary @- -o /dev/null -w 'channel: HTTP %{http_code}\n' "$SIGNOZ/api/v1/channels"
unset HC_SIGNALS_URL
jq -c '.[]' deploy/observability/signoz-alert-rules.json | while IFS= read -r RULE; do
  printf '%s' "$RULE" | curl -sS -K "$SZ_CFG" --data-binary @- -o /dev/null -w '%{http_code} ' "$SIGNOZ/api/v1/rules"
  printf '%s' "$RULE" | jq -r .alert
done
rm -f "$SZ_CFG"
```

Beklenen: `org ok`, `token ok`, `channel: HTTP 2xx` ve dosyadaki her kural için bir satır `2xx tappa <n>: …`. İlk
ikisinden biri gelmezse **dur** — sonraki çağrılar boş bir kimlikle gider. 🔴
**`send_resolved: true` zorunludur:** yoksa kapanış bildirimi hiç gönderilmez ve
`tappa-signals` ilk ateşten sonra **sonsuza dek** `down` kalır — sonraki her uyarı o
gürültünün içinde kaybolur. ⚠️ Canlı kanalda bu ayarın açık olduğu bu repodan
**doğrulanmadı**; SigNoz arayüzünün kanal ayarlarında `tappa-healthchecks`'i açıp gör.
Son olarak SigNoz → *Alerts*: dosyadaki bütün kurallar, durumları `inactive` (hatasız değerlendiriliyor)
— `firing` görünürse o gerçek bir uyarıdır. Kural JSON'unun `version` alanı `v5`'tir; bu
SigNoz `v4`'ü **reddediyor** (ölçüldü).

**Testler** (yalnız bu adımın; tam takım değil):
`go test ./cmd/tappa/ -run 'TestHeartbeat_|TestBackup_|TestBackupShip_|TestCronJobs_'` —
on üç test, adlarının öneki bunlardır. (f)'nin kuralları:
`go test ./cmd/tappa/ -run 'TestSignozRules_'` — beş test. ⚠️ Yanlış önekle süzülen bir
`-run` hiçbir testi koşmaz ve `ok` basar — yeşil ama boş.
🔴 **`Q28 (a)`:** M8-03'ün log sinyalleri 2026-10-11'den beri (f) üzerinden teslim
ediliyor (o gün yedisi; 8.si T122'de, 2026-10-13'te kümeye kuruldu);
kalanı — SigNoz'un tek başına ölümü, basitleştirmeler, tek kontrolün bütün kuralları
taşıması — sınır 25'te sayılı. `Q28 (b)` (log saklama) açık (sınır 24).

---

## Operator roles (M10 OP-5) — tek seferlik kurulum

**Ne:** migration `00026` (platform operatörü — ADR 0020/0021) iki **küme** rolüne
dayanır: `tappa_operator` (operatör yüzeyinin bağlanacağı rol; NOLOGIN, parolasız doğar)
ve `tappa_opdefiner` (`op_*` SECURITY DEFINER fonksiyonlarının sahibi; NOLOGIN,
BYPASSRLS, NOSUPERUSER, üyesi yok). Migration onları **yaratmaz** — roller küme
nesnesidir ve `scripts/redline-check.sh` R5b bir migration'daki BYPASSRLS rolü FAIL
eder (ADR 0021 §5). Tanımları `scripts/db-init/01-roles.sql`'dedir, ama o dosya **yalnız
boş PGDATA'da** koşar: taze kurulum, CI, `make db-reset` ve B YOLU geri yükleme onları
kendiliğinden alır; **halihazırda çalışan** bir veritabanı (geliştirme DB'si, canlı küme)
almaz. Bu bölüm o boşluğu bir kez kapatır.

**Tek kaynak, elle kopya yok.** Aşağıdaki komutlar `01-roles.sql`'in
`>>> OPERATOR ROLES (M10 OP-5) >>>` … `<<< OPERATOR ROLES (M10 OP-5) <<<` işaretleri
arasındaki bloğu `sed` ile keser ve psql'e verir — rolün ikinci bir yazımı yoktur. Blok
**idempotenttir**: var olan rollerde iki kez koşmak no-op'tur (ölçüldü, geliştirme DB'si,
2026-09-26: iki koşu da `DO · ALTER ROLE · ALTER ROLE · GRANT · GRANT · GRANT`, rc=0);
yanlış niteliklerle var olan bir rolü satırlarına indirger; `tappa_operator`'ın
LOGIN/parolasına **dokunmaz**. Bloğu ve migration'ın ön koşulunu
`internal/db/operatorschema_test.go` → `TestOperator00026_PreconditionRefusesAClusterWithoutTheRoles`
koşar (rolsüz kümede yaratır, var olanda no-op, yanlış nitelikte migration reddeder).

### 🔴 Sıra: ÖNCE bu bölüm, SONRA `main`'e birleştirme

Canlıda `00026`, deploy'un migrate Job'ında `tappa_owner` ile koşar. Roller yoksa ilk
ifadesi **düşer** ve bu bölümü adıyla gösterir. Geliştirme DB'sinde, roller yokken ölçüldü
(2026-09-26): `goose up` **exit 1**, son satır
`ERROR: migration 00026 (M10 OP-5) needs the cluster role(s) tappa_opdefiner, tappa_operator
and deliberately does not create them. Run the one-time step in deploy/README.md, section
"Operator roles (M10 OP-5)" … Nothing was changed. (SQLSTATE 55000)`, `goose_db_version`
önce ve sonra **25**.

**Sıra ters olursa ne olur** (`.github/workflows/deploy.yml` ve `deploy/k8s/`'ten okundu):
Job `backoffLimit: 2` ile üç kez aynı mesajla düşer → *"Migrate (its own step, its own
role, before any new binary serves)"* adımı `failed >= 3` görünce `exit 1` verir →
*"Roll out the server"* adımı **koşmaz** (varsayılan `if: success()`), yani
`deploy/k8s/20-app.yaml` uygulanmaz ve **eski pod servis vermeye devam eder** (Deployment'a
hiç dokunulmaz); goose her migration'ı kendi transaction'ında koştuğu için şema **25'te**
kalır. Deploy kırmızıdır, ürün ayaktadır. Kurtarma: aşağıdaki canlı adımı uygula, sonra
Actions → `deploy` → **Run workflow** (`workflow_dispatch`).

Kullanıcı, veritabanı ve parola **hiçbir komutta sabit yazılmaz**: psql, Postgres
konteynerinin kendi ortamındaki `POSTGRES_USER`, `POSTGRES_DB` ve `POSTGRES_PASSWORD`'ü
tek tırnaklı `sh -c` içinde okur (dışarıdaki kabuk genişletmez, parola ekrana basılmaz).
Canlı kümeye giden her `kubectl` satırı **`--context hetzner-k8s-1 -n tappa`** taşır —
yanlış bir varsayılan bağlamla başka bir kümeye rol yazmak bu bölümün tek geri
alınamayan hatası olurdu.

### Geliştirme veritabanı

```bash
sed -n '/^-- >>> OPERATOR ROLES (M10 OP-5) >>>/,/^-- <<< OPERATOR ROLES (M10 OP-5) <<</p' \
    scripts/db-init/01-roles.sql \
  | docker exec -i tappa-db \
      sh -c 'psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
make migrate
```

### Canlı küme (kullanıcı / orkestratör — ajan `kubectl` koşmaz)

Birleştirilecek commit'in checkout'unda, repo kökünde:

0) Kesimin kendisini doğrula. psql -1 BOŞ girdiyle exit 0 verir (ölçüldü, geliştirme DB'si) -- yani
işaretlerden biri yanlış yazılırsa 1. adım hiçbir şey yapmadan "başarılı" görünür. Blok iki CREATE
ROLE ve kapanış işaretini taşımalı:

```bash
sed -n '/^-- >>> OPERATOR ROLES (M10 OP-5) >>>/,/^-- <<< OPERATOR ROLES (M10 OP-5) <<</p' \
    scripts/db-init/01-roles.sql | grep -cE '^ *CREATE ROLE|^-- <<< OPERATOR ROLES'
```

beklenen: 3 (başka bir sayı -> DUR, 1. adımı koşma)

1) Bloğu canlı veritabanına uygula: tek transaction (-1), ilk hatada dur.

```bash
sed -n '/^-- >>> OPERATOR ROLES (M10 OP-5) >>>/,/^-- <<< OPERATOR ROLES (M10 OP-5) <<</p' \
    scripts/db-init/01-roles.sql \
  | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

beklenen: DO · ALTER ROLE · ALTER ROLE · GRANT · GRANT · GRANT

2) Doğrula

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT r.rolname, r.rolcanlogin, r.rolsuper, r.rolbypassrls,
       (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid) AS members,
       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid) AS member_of,
       has_schema_privilege(r.rolname, 'public', 'USAGE') AS usage,
       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid) AS role_settings
  FROM pg_roles r
 WHERE r.rolname IN ('tappa_operator', 'tappa_opdefiner')
 ORDER BY 1;
SQL
```

beklenen (geliştirme DB'sinde ölçülen şekil):

```text
tappa_opdefiner|f|f|t|0|0|t|0
tappa_operator|f|f|f|0|0|t|0
```

("Operator surface (M10 OP-7)" bölümünün 2. adımından SONRA tappa_operator'ın ikinci sütunu t olur —
giriş o adımda açılır; öteki sütunlar aynı kalmalı.) Son sütun 0 olmalı: tappa_operator kendi rol
varsayılanını yazabilir (ör. log_parameter_max_length_on_error -- `user` bağlamlı bir ayar; -1
reddedilen her op_* çağrısının bağlı parametrelerini log'a ve çağırana döker). 0 değilse DUR: satırı
gör (`SELECT setconfig FROM pg_db_role_setting WHERE setrole = 'tappa_operator'::regrole`) ve
`ALTER ROLE ... RESET ...` ile kaldır; bkz. ADR 0021 "Karar verilmedi".

⚠️ İstisna: ADR 0021 "Karar verilmedi" → "Commit'ten bağımsız ikinci iz" seçeneği (log_statement +
log_parameter_max_length) BİR ROL AYARIDIR. Benimsenirse bu adım ve ters katalog pini aynı
değişiklikte güncellenir, izin verilen satır burada adıyla yazılır; o gün gelene kadar beklenen
değer 0'dır.

Sonra `main`'e birleştir; deploy 00026'yı uygular.

### Bu bölümün AÇMADIĞI şey: `tappa_operator`'ın girişi

Bu adımdan sonra `tappa_operator` hâlâ **NOLOGIN ve parolasızdır** — bilerek
(`tappa_app`'in ölçülmüş fail-open düzeltmesi, `01-roles.sql` başlığı). Operatör
yüzeyinin bağlantısı OP-7 ile gelir; girişi açan adım da onunla birlikte yazılır. Adlar
(değerler **hiçbir dosyaya** yazılmaz — Olay A-0; ADR 0021 §5): kullanıcı
`tappa-secrets`'a `TAPPA_OPERATOR_DATABASE_URL`, `TAPPA_OPERATOR_TOTP_KEK`,
`TAPPA_OPERATOR_TOKEN_HMAC_KEY` ve **`tappa_operator` rolünün parolasını** ekler.
⚠️ Parolayı role veren adım **`tappa-secrets`'ı yerel makineye okumamalıdır** (bu dosya
`tappa-secrets` üzerinde `jsonpath`'i yasaklıyor, §4.7): emsali
`deploy/k8s/postgres-init/02-app-password.sh` — değer bir pod'a `secretKeyRef` ile gelir
ve `psql -v` değişkeniyle, kabuk genişletmesi olmadan verilir. Hangi pod'un bunu yapacağı
ve anahtarın adı OP-7'nin kararıdır. → **OP-7'de karara bağlandı (2026-10-01):** aşağıdaki
*"Operator surface (M10 OP-7)"* bölümü. Parola **ayrı bir Secret anahtarı değildir** (yalnız
DSN'in içinde durur) ve role bir pod'un `secretKeyRef`'iyle değil, psql'in `\password`
komutuyla **stdin'den** verilir — `tappa-secrets`'ın hiçbir **değeri** ekrana gelmez (tek
okuma o bölümün 3a adımındaki `kubectl describe`'dır: yalnız anahtar adı ve bayt boyu).

**Geri alma yok.** `00026`'nın Down'ı operatör hesaplarını, oturumlarını ve **operatör
audit izinin tamamını** siler (migration'ın kendi uyarısı); canlıda koşulmaz. Roller
bırakılabilir: NOLOGIN, üyesiz ve (00026 olmadan) hiçbir nesnenin sahibi değil.

---

## Holding role (M11 API-1a) — tek seferlik kurulum

**Ne:** migration `00041` (holding varlığı — ADR 0027 §1–§3) bir **küme** rolüne dayanır:
`tappa_holdingdefiner` — kapalı `holding_*` listesinin ve holding tetikleyicilerinin sahibi;
NOLOGIN, BYPASSRLS, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, **üyesi yok ve
hiçbir rolün üyesi değil**. Migration onu **yaratmaz** (rol küme nesnesidir; R5b bir
migration'daki BYPASSRLS rolünü FAIL eder — *"Operator roles (M10 OP-5)"* bölümünün
gerekçesi). Tanımı `scripts/db-init/01-roles.sql`'in
`>>> HOLDING ROLE (M11 API-1a) >>>` … `<<< HOLDING ROLE (M11 API-1a) <<<` bloğudur: taze
kurulum, CI, `make db-reset` ve **B YOLU geri yüklemesinin sıfırdan kurulan pod'u** onu
kendiliğinden alır (`pg_dump` rol taşımaz — rol pod'un init'inden gelir); **halihazırda
çalışan** bir veritabanı almaz. Blok **idempotenttir** (var olan rolde no-op; yanlış
nitelikleri satırına indirger; üyeliği temizlemez — `00041`'in ön koşulu iki yönü de sınar ve
reddeder). Bloğu ve ön koşulu `internal/db` → `TestRestore_FreshPodCarriesTheHoldingRole`
koşar.

**Sıra:** önce bu bölüm, sonra `main`'e birleştirme. Rol yoksa `00041`'in ilk ifadesi bu
bölümü adıyla gösterip **düşer** (`SQLSTATE 55000`, *"Nothing was changed."*); migrate Job'ı
kırmızıdır, eski pod servis vermeye devam eder — *"Operator roles (M10 OP-5)"* bölümündeki
*"Sıra ters olursa ne olur"* paragrafının aynısı.

### Geliştirme veritabanı

```bash
sed -n '/^-- >>> HOLDING ROLE (M11 API-1a) >>>/,/^-- <<< HOLDING ROLE (M11 API-1a) <<</p' \
    scripts/db-init/01-roles.sql \
  | docker exec -i tappa-db \
      sh -c 'psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
make migrate
```

Ölçüldü (M11 geliştirme veritabanı, 2026-10-13): `DO · ALTER ROLE · GRANT`, rc=0; sonra rol
`NOLOGIN, NOSUPERUSER, BYPASSRLS, NOCREATEDB, NOCREATEROLE, NOREPLICATION`.

### Canlı küme (kullanıcı / orkestratör — ajan `kubectl` koşmaz)

0) Kesimi doğrula (boş girdiyle psql -1 exit 0 verir): bir CREATE ROLE ve kapanış işareti.

```bash
sed -n '/^-- >>> HOLDING ROLE (M11 API-1a) >>>/,/^-- <<< HOLDING ROLE (M11 API-1a) <<</p' \
    scripts/db-init/01-roles.sql | grep -cE '^ *CREATE ROLE|^-- <<< HOLDING ROLE'
```

beklenen: 2 (başka bir sayı -> DUR)

1) Uygula: tek transaction (-1), ilk hatada dur.

```bash
sed -n '/^-- >>> HOLDING ROLE (M11 API-1a) >>>/,/^-- <<< HOLDING ROLE (M11 API-1a) <<</p' \
    scripts/db-init/01-roles.sql \
  | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

beklenen: DO · ALTER ROLE · GRANT

2) Doğrula

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT r.rolname, r.rolcanlogin, r.rolsuper, r.rolbypassrls,
       (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid) AS members,
       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid) AS member_of,
       has_schema_privilege(r.rolname, 'public', 'USAGE') AS usage,
       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid) AS role_settings
  FROM pg_roles r
 WHERE r.rolname = 'tappa_holdingdefiner';
SQL
```

beklenen: tappa_holdingdefiner|f|f|t|0|0|t|0

Sonra `main`'e birleştir; deploy `00040`–`00042`'yi uygular.

**Geri alma:** `00042`'nin Down'ı operatör yüzeyini (`op_*`'lar, türler) geri alır; `00041`'in
Down'ı holding tablolarını **üyelikleriyle birlikte** düşürür (izleri operatörün ve
tenant'ların append-only audit satırlarında kalır) ve `tenants` üzerinde ACCESS EXCLUSIVE
alır — canlıda koşulmaz. Rol bırakılabilir: NOLOGIN, üyesiz, (`00041` olmadan) hiçbir nesnenin
sahibi değil.

### Holding tablolarına dokunan migration — `lock_timeout` kuralı (M11 API-1b, API-1c)

**Kural:** `holding_members`'a ya da kapının (`holding_site_gate`, migration `00043`) kendi
`lock_timeout`'u altında okuduğu `tags`'e — **ve (API-1c) izdüşümün (`holding_feed`, migration
`00046`) kendi `lock_timeout`'u altında yazdığı `site_punches`'a ya da anahtarıyla `KEY SHARE`
kilitlediği sahanın `tenants` satırına** (`locations` ve `tags` satırlarını o işlemde önce
`transactions`'ın kendi anahtarları kilitler — aşağıda) — kilit alan bir migration,
ilk ifadesinde kendi `lock_timeout`'unu **2 sn'nin altına** koyar ve kilit altındaki işini kısa tutar:

```sql
-- +goose Up
SET LOCAL lock_timeout = '1s';   -- kapının 2 sn'sinin altında: bekleyen taraf migration olsun
```

**Neden (ADR 0027 §13 madde 16, ölçüldü):** kapı bir site satırı yazılırken üyelik satırlarını
`FOR SHARE` ile en çok 2 sn bekler, checkin bir kez yeniden dener; tablo bir migration'ın elindeyse
(ya da kilit kuyruğunda bir ACCESS EXCLUSIVE bekliyorsa) bekleyen **dokunuştur**, ve ilerletilmiş bir
NFC dokunuşunda iki bekleme dolarsa sayaç ilerlemiş, kayıt yazılmamış olur. `lock_timeout`'u kısa
olan migration ise kilidi alamazsa **kendisi** düşer: migrate Job'ı `Failed`, rollout yapılmaz
(*"Olay müdahalesi"* madde 4) ve migration sakin bir anda yeniden koşulur — kayıt kaybolmaz.
Kilidi aldıktan sonraki iş de 2 sn'nin altında kalmalıdır (ölçü emsali:
`TestHoldingMigration_AlterWithinTwoSeconds` — `00043`'ün ALTER'ı 2 M satırda 0,39–0,46 sn).
`VACUUM FULL` ve `CLUSTER` bu tablolarda trafik akarken koşulmaz; elle `LOCK` da öyle — ve elle bir
`SELECT … FROM tenants … FOR UPDATE` da (izdüşümün `tenants` anahtarı onu bekler), `site_punches`'ta
`CONCURRENTLY` olmayan bir `CREATE INDEX` ya da `REINDEX` da. **`tags` / `locations` üzerinde elle bir
`FOR UPDATE` daha ağırdır:** o satırlara `KEY SHARE`'i `holding_feed`'den önce `transactions`'ın kendi
anahtarları alır (`transactions_site_tag_fk`, `transactions_site_location_fk`) — orada `lock_timeout` ve
yeniden deneme yoktur, dokunuş isteğin zaman aşımına (30 sn) kadar bekler (ikinci API-1c denetimi ölçtü;
M11'den eski sınıf, ADR 0027 §13 madde 16 ve 18). Ölçüldü (API-1c,
ADR 0027 §13 madde 18): `site_punches` 3 sn `SHARE` kilitliyken site satırının ilk denemesi 2,02 sn'de
55P03, checkin'in savepoint'li tek yeniden denemesi kaydı yazdı; iki bekleme (> 4 sn) dolarsa kayıt
kaybolur (`TestHoldingWindow_ProjectionLockHeldThroughBothWaitsIsTheNamedCost`).

**`audit_log` üzerinde indeks — `00054` (M11 API-6):** `audit_log_invite_delivery_idx` goose'un işleminde
(`CONCURRENTLY` değil) kurulur ve kurulum süresince `audit_log`'u `SHARE` kilitler; ilk ifadesi `SET LOCAL
lock_timeout = '1s'`. Ölçüm (API-6 denetimi): ~1 M `audit_log` satırında kurulum ≈ 0,8 sn. **Eşik:** prod'da
`audit_log` ~2,5 M satırı aşıyorsa `00054` bu hâliyle koşulmaz — indeks `CONCURRENTLY` ile, işlemsiz ayrı bir
migration olarak kurulur (API-13 runbook'u satır sayısını deploy'dan önce ölçer). Gerekçe yukarıdaki kural:
`op_remove_holding_member` `audit_log`'u beklerken üyelik satırını tutar (ADR 0027 §13 madde 16).

**`employees` üzerinde indeks — `00056` (M11 API-8):** `employees_tenant_updated_idx (tenant_id, updated_at, id)` goose'un
işleminde (`CONCURRENTLY` değil) kurulur ve kurulum süresince `employees`'i `SHARE` kilitler — çalışan yazmaları (panel,
API, davet kabulü) bekler; dokunuşlar `employees`'i yalnız okur, beklemez. İlk ifadesi `SET LOCAL lock_timeout = '1s'`
(açık bir çalışan yazmasının arkasında 55P03 ile düşer — `TestEmployees00056_GivesUpAfterASecondBehindAnOpenWrite`). Ölçüm
(API-8, atılabilir kap): 42 M satırda kurulum 30,5–38,2 sn (≈ 0,8 sn / 1 M satır). **Eşik:** prod'da `employees` ~2 M satırı
aşıyorsa `00056` bu hâliyle koşulmaz — indeks `CONCURRENTLY` ile, işlemsiz ayrı bir migration olarak önce kurulur (API-13
runbook'u satır sayısını deploy'dan önce ölçer). Önündeki `00055` (`holding_directory`, `holding_site_locations`'ın gövdesi —
`CREATE OR REPLACE`) tablo kilidi almaz.

**`employees` / `locations` / `tenants` üzerinde `ALTER` — `00037`, `00038` (M11 API-1; API-13 güvenlik turu):** ikisi de
bu tablolarda `ACCESS EXCLUSIVE` alır (sütun/kısıt ekleme; Down'larında silme). Up'larının **ve** Down'larının ilk ifadesi
`SET LOCAL lock_timeout = '1s'`: uzun bir okuyucunun (02:30 yedeğinin dökümü, açık bir panel işlemi) arkasında 55P03 ile
düşerler — migrate Job'ı `Failed`, rollout yapılmaz, sakin bir anda yeniden koşulur — yoksa sonraki her okuma ve dokunuş
kilidin arkasında kuyruğa girerdi. Kilidi aldıktan sonra `ADD COLUMN`'lar tabloyu yeniden yazmaz (sabit varsayılan); `00037`'nin
iki benzersiz indeksi `employees`'in satır sayısıyla orantılıdır.
Pin: `TestM11Migrations00037And00038_OpenWithASecondsLockTimeout`. **Deploy saati:** 02:30 yedeğinin penceresinden uzak.

**`webhook_deliveries` — `00058` ve `00059` (T130, teslim satırlarının 90 günlük saklaması):** tablo her dokunuşta yazılır —
`00049`'un tetikleyicileri teslim satırını dokunuşun işleminde, kendi **2 sn**'lik `lock_timeout`'u altında ekler. `00058`
(`settled_at` sütunu, onun tetikleyicisi, kısıtlayıcı DELETE politikası, `tappa_app`'e DELETE) tabloda `ACCESS EXCLUSIVE` alır;
sütun **sabit olmayan bir varsayılanla** (`DEFAULT now()`) eklenir — katalogda bir kez saklanır, **hiçbir satır yeniden yazılmaz** —
ve kilit altındaki tek veri işi bekleyen satırların (canlı kuyruk) `UPDATE`'idir. `00059` (`webhook_deliveries_tenant_settled_idx`)
goose'un işleminde (`CONCURRENTLY` değil) kurulur ve kurulum süresince tabloyu `SHARE` kilitler — teslim satırı yazan dokunuş bekler.
İkisinin de Up'ının **ve** Down'ının ilk ifadesi `SET LOCAL lock_timeout = '1s'`: açık bir teslim yazmasının arkasında 55P03 ile
düşerler — migrate Job'ı `Failed`, rollout yapılmaz, sakin bir anda yeniden koşulur
(`TestWebhookRetention00058And00059_GiveUpAfterASecondBehindAnOpenWrite`). Ölçüm (T130, atılabilir kap, PostgreSQL 17, disk; satırların
binde biri bekleyen): `00058` 100 B satırda 15 ms, 1 M'de 32–63 ms, 3 M'de 67–410 ms; `00059` 100 B'de 56–63 ms, 1 M'de 0,66–0,69 sn,
3 M'de 2,2–2,3 sn. **Eşik:** prod'da `webhook_deliveries` ~1,5 M satırı aşıyorsa `00059` bu hâliyle koşulmaz — indeks `CONCURRENTLY`
ile, işlemsiz (`-- +goose NO TRANSACTION`) ayrı bir migration olarak önce kurulur; deploy'dan önce satır sayısı ölçülür
(`SELECT count(*) FROM webhook_deliveries`). Bugün tablo pilotun, günlerce eski. **Deploy sonrası:** süpürme işletmenin kendi
turunda koşar (K-19; tarama yok) — bir dokunuştan sonra süreç log'unda doksan günü geçmiş satır varsa
`webhook: settled deliveries past the retention deleted` (yalnız `tenant_id` ve sayı), başarısızlıkta
`webhook: the delivery log's retention sweep failed` (yalnız `tenant_id` ve SQLSTATE sınıfı); ikisi de teslimi durdurmaz.

## Feed role (M11 API-2) — tek seferlik kurulum

**Ne:** migration `00048` (punch akışının tabloları — ADR 0028 §4, §5) bir **küme** rolüne dayanır:
`tappa_feedwriter` — kendi-tenant akış tetikleyicisinin (`punch_feed`) ve yeni tenant'ın sayaç
satırını açan tetikleyicinin (`feed_open_counter`) sahibi; NOLOGIN, **NOBYPASSRLS**, NOSUPERUSER,
NOCREATEDB, NOCREATEROLE, NOREPLICATION, **üyesi yok ve hiçbir rolün üyesi değil**. Tenant sınırını
aşmaz (yazdığı her satır RLS `WITH CHECK`'inden geçer); yalnız olay yazma hakkını tetikleyiciye
kilitler — `tappa_app` olay yazamaz. Migration onu **yaratmaz** (rol küme nesnesidir — *"Holding
role (M11 API-1a)"* bölümünün gerekçesi). Tanımı `scripts/db-init/01-roles.sql`'in
`>>> FEED ROLE (M11 API-2) >>>` … `<<< FEED ROLE (M11 API-2) <<<` bloğudur: taze kurulum, CI,
`make db-reset` ve **B YOLU geri yüklemesinin sıfırdan kurulan pod'u** onu kendiliğinden alır;
**halihazırda çalışan** bir veritabanı almaz. Blok **idempotenttir**; üyeliği temizlemez —
`00048`'in ön koşulu iki yönü de sınar ve reddeder. Bloğu ve ön koşulu `internal/db` →
`TestRestore_FreshPodCarriesTheFeedRole` koşar.

**Sıra:** önce bu bölüm, sonra `main`'e birleştirme. Rol yoksa `00048`'in ilk ifadesi bu bölümü
adıyla gösterip **düşer** (`SQLSTATE 55000`, *"Nothing was changed."*).

### Geliştirme veritabanı

```bash
sed -n '/^-- >>> FEED ROLE (M11 API-2) >>>/,/^-- <<< FEED ROLE (M11 API-2) <<</p' \
    scripts/db-init/01-roles.sql \
  | docker exec -i tappa-db \
      sh -c 'psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
make migrate
```

### Canlı küme (kullanıcı / orkestratör — ajan `kubectl` koşmaz)

0) Kesimi doğrula (boş girdiyle psql -1 exit 0 verir): bir CREATE ROLE ve kapanış işareti.

```bash
sed -n '/^-- >>> FEED ROLE (M11 API-2) >>>/,/^-- <<< FEED ROLE (M11 API-2) <<</p' \
    scripts/db-init/01-roles.sql | grep -cE '^ *CREATE ROLE|^-- <<< FEED ROLE'
```

beklenen: 2 (başka bir sayı -> DUR)

1) Uygula: tek transaction (-1), ilk hatada dur.

```bash
sed -n '/^-- >>> FEED ROLE (M11 API-2) >>>/,/^-- <<< FEED ROLE (M11 API-2) <<</p' \
    scripts/db-init/01-roles.sql \
  | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

beklenen: DO · ALTER ROLE · GRANT

2) Doğrula

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT r.rolname, r.rolcanlogin, r.rolsuper, r.rolbypassrls,
       (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid) AS members,
       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid) AS member_of,
       has_schema_privilege(r.rolname, 'public', 'USAGE') AS usage,
       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid) AS role_settings
  FROM pg_roles r
 WHERE r.rolname = 'tappa_feedwriter';
SQL
```

beklenen: tappa_feedwriter|f|f|f|0|0|t|0

Sonra `main`'e birleştir; deploy `00048`–`00050`'yi uygular. `00049` `transactions` ve
`transaction_reviews`'u `SHARE ROW EXCLUSIVE` kilitler (kendi `lock_timeout`'u 1 sn — aşağıdaki kural) ve
yalnız tetikleyicileri kurar (kilit altındaki iş milisaniyeler; geçmiş yazmaz). Kilit 1 sn içinde
alınamazsa migrate Job'ı düşer, sakin bir anda yeniden koşulur — kayıt kaybolmaz. `00050`
(`NO TRANSACTION`) geçmişi tenant başına en çok 1 000 punch'lık işlemlerle geri doldurur; her işlem
yalnız o tenant'ın sayaç satırını tutar, tutma süresi tenant'ın büyüklüğünden bağımsızdır
(`TestFeedBackfill_BigTenantLiveTapsLoseNothing`) ve istatistiğe değil sorgunun metnine bağlıdır
(`TestFeedBackfill_ThePlanIsTheTexts`); büyük bir veritabanında migrate Job'ı dakikalar sürebilir
(atılabilir kapta 650 000 satırlık tek tenant: ~40 sn), dokunuşlar bu sırada yazılmaya devam eder.

**Geri alma:** `00049`'un Down'ı yazıcıları kaldırır ve `holding_feed`'i `00046`'nın gövdesine döndürür;
yazılmış olaylar **kalır** (append-only). Down süresince yazılan kayıtların olayı yoktur; `00049`'un ve
`00050`'nin bir sonraki Up'ı onları sayaçtan sonraki `seq`'lerle geri doldurur (kayıp yok, sıra sonra). `00048`'in Down'ı akışta
olay, uç nokta ya da teslim satırı varken **reddeder** — canlıda koşulmaz.

### Akış sayaçlarına dokunan el işi — `lock_timeout` kuralı

İki tetikleyici sayaç satırını kendi `lock_timeout`'u (2 sn) altında kilitler ve checkin bir kez
yeniden dener (ADR 0027 §13 madde 23). Bu yüzden `feed_counters`'a ya da `punch_events`'e kilit alan
bir migration ilk ifadesinde kendi `lock_timeout`'unu **2 sn'nin altına** koyar (yukarıdaki holding
kuralının aynısı); trafik akarken bu tablolarda elle `SELECT … FOR UPDATE`, `LOCK`, `VACUUM FULL`,
`CLUSTER` ya da `CONCURRENTLY` olmayan `CREATE INDEX` koşulmaz. Geri yükleme adımının
`UPDATE public.feed_counters …`'u uygulama **kapalıyken** koşar (aşağıda).

---

## Identity role (M14-03) — tek seferlik kurulum

**Ne:** migration `00069` (panel yöneticisinin kimliği — ADR 0031 §5.1) bir **küme** rolüne dayanır:
`tappa_identitydefiner` — kapalı `identity_*` listesinin (ADR 0031 §5.2; fonksiyonlar M14-04'ten
itibaren gelir) sahibi; NOLOGIN, **BYPASSRLS**, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION,
**üyesi yok, hiçbir rolün üyesi değil, varsayılan yetkisi yok**; yetkisi yalnız migration'ların tek tek
verdiği sütun grant'larıdır (`00069` ona hiçbir şey vermez). `admin_users`'ın bağlı satır tetikleyicisi
(`00069`) bu rolün **adını** okur: bağlı ya da reddedilmiş bir satırın parolasını yalnız bu rol olarak
koşan bir gövde değiştirir. Migration rolü **yaratmaz** (rol küme nesnesidir — *"Holding role (M11
API-1a)"* bölümünün gerekçesi; migration'da `CREATE ROLE` R5b'yi kırar). Tanımı
`scripts/db-init/01-roles.sql`'in `>>> IDENTITY ROLE (M14-03) >>>` … `<<< IDENTITY ROLE (M14-03) <<<`
bloğudur: taze kurulum, CI, `make db-reset` ve **B YOLU geri yüklemesinin sıfırdan kurulan pod'u** onu
kendiliğinden alır; **halihazırda çalışan** bir veritabanı almaz. Blok **idempotenttir**; üyeliği
temizlemez — `00069`'un ön koşulu iki yönü, varsayılan yetkiyi ve rolün önceden sahip olduğu her tablo
yetkisini sınar ve reddeder. Bloğu ve ön koşulu `internal/db` →
`TestAdminIdentities00069_DownRefusesAnIdentityAndOtherwiseWalksBackAndUp` koşar.

**🔴 Sıra:** önce bu bölüm (rol + `USAGE` GRANT'ı), **sonra** `main`'e birleştirme. Rol yoksa `00069`'un
ilk ifadesi bu bölümü adıyla gösterip **düşer** (`SQLSTATE 55000`, *"Nothing was changed."*) —
migrate Job'ı `Failed`, rollout yapılmaz, eski pod servis vermeye devam eder.

### Geliştirme veritabanı

```bash
sed -n '/^-- >>> IDENTITY ROLE (M14-03) >>>/,/^-- <<< IDENTITY ROLE (M14-03) <<</p' \
    scripts/db-init/01-roles.sql \
  | docker exec -i tappa-db \
      sh -c 'psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
make migrate
```

### Canlı küme (kullanıcı / orkestratör — ajan `kubectl` koşmaz)

0) Kesimi doğrula (boş girdiyle psql -1 exit 0 verir): bir CREATE ROLE ve kapanış işareti.

```bash
sed -n '/^-- >>> IDENTITY ROLE (M14-03) >>>/,/^-- <<< IDENTITY ROLE (M14-03) <<</p' \
    scripts/db-init/01-roles.sql | grep -cE '^ *CREATE ROLE|^-- <<< IDENTITY ROLE'
```

beklenen: 2 (başka bir sayı -> DUR)

1) Uygula: tek transaction (-1), ilk hatada dur.

```bash
sed -n '/^-- >>> IDENTITY ROLE (M14-03) >>>/,/^-- <<< IDENTITY ROLE (M14-03) <<</p' \
    scripts/db-init/01-roles.sql \
  | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

beklenen: DO · ALTER ROLE · GRANT

2) Doğrula

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT r.rolname, r.rolcanlogin, r.rolsuper, r.rolbypassrls,
       (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid) AS members,
       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid) AS member_of,
       has_schema_privilege(r.rolname, 'public', 'USAGE') AS usage,
       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid) AS role_settings,
       (SELECT count(*) FROM pg_default_acl d WHERE d.defaclrole = r.oid
                                                OR d.defaclacl::text ~ '(^|[{,])tappa_identitydefiner=') AS default_acl
  FROM pg_roles r
 WHERE r.rolname = 'tappa_identitydefiner';
SQL
```

beklenen: tappa_identitydefiner|f|f|t|0|0|t|0|0

Sonra `main`'e birleştir; deploy `00069` ve `00070`'i uygular.

- **`00069`** `admin_sessions` ve `admin_users`'ta `ACCESS EXCLUSIVE` alır (sütun ekleme, CHECK, yabancı
  anahtar, tetikleyici) ve `tenants`'ta `SHARE ROW EXCLUSIVE` (yeni tablonun `tenants`'a yabancı
  anahtarı — bir tenant güncellemesini bekletir, dokunuşun okumasını değil). Her ifadeden önce kendi
  `SET LOCAL lock_timeout = '1s'`'i var: uzun bir okuyucunun (02:30 dökümü, açık bir panel işlemi)
  arkasında 55P03 ile düşer — migrate Job'ı `Failed`, rollout yapılmaz, sakin bir anda yeniden koşulur.
  Kilit altındaki iş `admin_users`'ın bir taramasıdır (CHECK ve yabancı anahtar; `identity_id` hepsi
  `NULL`) — birkaç bin satır. **Deploy saati:** 02:30 yedeğinin penceresinden uzak.
- **`00070`** `UNIQUE (tenant_id, identity_id)` indeksini `CONCURRENTLY` kurar (`NO TRANSACTION`):
  hiçbir istek onu beklemez; kendisi kendinden eski işlemleri bekler (uzun bir okuyucu onu uzatır,
  paneli değil). Yarıda kesilirse geride `INVALID` bir indeks kalır; dosya yeniden koşulunca önce onu
  düşürür, sonra yeniden kurar (goose `NO TRANSACTION` dosyasını yalnız sonunda kaydeder).

- **`00072`** (M14-05) üç tenant-kapsamsız tabloyu (`admin_identity_resets`, `_verifications`,
  `_events`), kimlik fonksiyonlarını ve `resolve_admin_by_email`'in yeni tanımını getirir. Sıcak tabloda
  kilit yok (yalnız sütun GRANT'ı — GRANT tablo kilidi almaz); yeni tabloların `admin_identities`'e
  yabancı anahtarı orada `SHARE ROW EXCLUSIVE` alır. `resolve_admin_by_email` `DROP` + `CREATE` ile tek
  transaction'da değişir: o an giriş yapan istek fonksiyonun kilidini bekler, eski binary'nin sorgusu
  (dört sütun, adıyla) yeni tanımda çalışır. Ön koşul: `pgcrypto` (`crypt`, `gen_salt`,
  `gen_random_bytes` — `scripts/db-init/01-roles.sql`'in uzantısı) `public`'te; yoksa dosya hiçbir şey
  değiştirmeden reddeder. Down, üç tablodan birinde satır ya da reddedilmiş (`unlinked_at`) bir üyelik
  varken **reddeder** — canlıda koşulmaz; geri dönüş önceki binary'dir.

  **Expand deploy'undan ÖNCE, üretimde, salt okuma ön kontrolü** (kullanıcı koşar; 00072 `pgcrypto`'ya
  dayanan ilk migration'dır — `00027` ondan bilerek kaçındı). Önce bu bölümün "Identity role (M14-03)"
  adımıyla `tappa_identitydefiner` yaratılmış olmalı. `tappa_owner` ile, `BEGIN READ ONLY` içinde:

  ```sql
  BEGIN READ ONLY;
  SELECT extnamespace::regnamespace FROM pg_extension WHERE extname = 'pgcrypto';
  SELECT has_function_privilege('tappa_identitydefiner', 'public.crypt(text,text)', 'EXECUTE'),
         has_function_privilege('tappa_identitydefiner', 'public.gen_salt(text,integer)', 'EXECUTE'),
         has_function_privilege('tappa_identitydefiner', 'public.gen_random_bytes(integer)', 'EXECUTE');
  ROLLBACK;
  ```

  beklenen: `public` ve `t|t|t`. `crypt(text,text)` hiç yoksa `has_function_privilege` sonuç vermez,
  **hata** verir — o da "deploy yok" demektir. Satır yoksa ya da `f` varsa deploy yapılmaz (migration zaten hiçbir şey
  değiştirmeden reddeder; Job `Failed` olur): uzantı `01-roles.sql`'deki gibi `public`'te kurulur ya da
  yetki düzeltilir, sonra yeniden denenir.

  **İkinci ön kontrol — adres kuralı (M14-08a, ADR 0031 §5.1'in 2026-10-26 notu; kullanıcı koşar,
  salt okuma, yalnız sayı).** `00073`'ün backfill'i her dolu e-posta grubuna, grubun seçilen satırının
  **yazımıyla** kimlik doğurur ve citext U+0130'u (`İ` → `i`) ve U+212A'yı (KELVIN SIGN → `k`) ASCII'ye
  katlar: yazdırılabilir ASCII olmayan bir satır seçilirse kimlik postanın gidemeyeceği bir yazımla
  doğar ve gerçek posta kutusunun sahibi o adrese bir daha sıfırlama alamaz. `00073` uygulanmış bir
  migration'dır (geliştirme ve CI veritabanlarında) ve değiştirilmez (CLAUDE.md §6); kuralın backfill
  tarafı bu sayımdır. `tappa_owner` ile, backfill'in aday kümesi (`email` dolu ve boş değil) üzerinde:

  ```sql
  BEGIN READ ONLY;
  SELECT count(*)                                                AS backfill_candidates,
         count(*) FILTER (WHERE email::text !~ '^[\x21-\x7e]+$') AS not_printable_ascii,
         count(*) FILTER (WHERE NOT (octet_length(email::text) <= 254
                                AND strpos(email::text, '=?') = 0
                                AND email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$')) AS failing_send_rule
    FROM admin_users
   WHERE email IS NOT NULL AND btrim(email::text) <> '';
  ROLLBACK;
  ```

  (Üçüncü sayımın yüklemi `00076`'nın 6b'sidir, `v_email` yerine `email`; desen dosyadaki ile bayt
  bayt aynıdır — `TestIdentityAddressRule_TheRunbookCountsWithThePattern`.)

  beklenen: `not_printable_ascii` = **0** (M14-02'nin 2026-10-22 sayılarıyla `4|0|0`). **0 değilse deploy
  yapılmaz — DUR**: o satırlar adıyla değil yalnız sayıyla bilinir (adres log'a ve bu dosyaya yazılmaz,
  CLAUDE.md §7); ne yapılacağı kullanıcının kararıdır. `email::text` dönüşümü şarttır: citext'in kendi
  `~`'ı harf büyüklüğüne duyarsızdır. İkinci sayım `mail.ValidRecipient`'in ASCII yarısıdır (0x21–0x7e;
  boşluk, denetim karakteri, DEL ve 0x80 üstü her bayt sayılır); homoglif tehdidi tam bu kümedir. **Üçüncü
  sayım kuralın tamamıdır:** ikinciye ek olarak ASCII ama biçimi bozuk adresler (`a..b@x.mt`, `"q"@x.mt` —
  eski `deliverableShape`'in geçirdiği şekiller). Böyle bir satır citext'te başka bir adrese katlanmaz
  (ASCII'de citext yalnız harf büyüklüğünü katlar), yani gasp değildir; ama backfill ona postanın
  gidemeyeceği bir adresle kimlik doğurur. Bu **adlandırılmış kalıntıdır** (ADR 0031 §5.8'in M14-08a
  notu): kendi başına DUR değildir, `failing_send_rule − not_printable_ascii` sayısı deploy'dan önce
  kullanıcıya bildirilir. **Pencere `00076`'da kapanır:** bu sayım ile `00073`'ün koşması arasında eski
  binary hâlâ her UTF-8 adresi alır; `00076`'nın Up'ı aynı goose koşusunda yazdırılabilir ASCII olmayan
  bir kimlik görürse **yalnız sayıyla** hata verir — migrate Job `Failed`, rollout yok, karar
  kullanıcının. Ölçüm (2026-10-27, bu dalın veritabanı, goose 76): `293|0|0` (testlerin bıraktığı satırlarla); geri alınan bir
  transaction'da U+0130'lu, U+212A'lı ve başı boşluklu üç satır ve `a..b@` biçimli bir satır eklenince
  `298|3|4`, düz ASCII olan sayılmadı.

**Eski binary yeni şemada çalışır** (ADR 0031 §8 expand): kayıt `identity_id NULL` ile satır yazar,
oturum `authenticated_at`'i varsayılandan alır; `tappa_app`'ın `admin_users` okuması sütun listesine
indi ama bugünkü hiçbir sorgu o listenin dışını okumaz
(`TestAdminUsers00069_EveryGeneratedStatementOnTheAdminTablesStillPlansAsTheApp`). **Adlandırılmış
bedel:** bağlı bir satıra (M14-06'nın backfill'inden sonra) eski pod'un parola değişimi ya da sıfırlama
tüketimi tetikleyiciyle reddedilir (23001 → 500) — rollout penceresinde birkaç dakika (ADR 0031 §5.1).

**Geri alma:** expand'den geri dönüş **önceki binary'dir** (şemayla çalışır), Down değil. `00069`'un
Down'ı `admin_identities`'te tek bir kimlik ya da bağlı / reddedilmiş tek bir satır varken **reddeder**
(kimliğin tek kopyası ve bağlar düşerdi) — canlıda koşulmaz. Rol Down'la gitmez (küme nesnesi).

### Backfill (M14-06) — `00073`

Expand deploy'unun migrate Job'ında, rollout'tan **önce**, eski binary canlıyken koşar (ADR 0031 §5.8,
§8). Her dolu e-posta grubuna bir kimlik doğar ve grubun **tek** satırı ona bağlanır
(`linked_via='backfill'`, `linked_at=now()`): **en eski aktif** satır, aktif satır yoksa en eskisi
(eşitlikte `tenant_id`, sonra `id`). Kimlik o satırın digest'iyle doğar; grubun öbür satırları
bağlanmamış kalır; e-postasız (ya da boş) satıra kimlik yok. Migration `tappa_owner` ile koşar; ayrı bir
adım yok.

- **Rol iddiası:** koşan rol superuser ya da BYPASSRLS değilse dosya hiçbir şey yazmadan **hata** verir
  (FORCE RLS altında 0 satır görüp "başarılı" kaydedilirdi). Canlıda `tappa_owner` `rolsuper = t`,
  `rolbypassrls = t` (M14-02 ölçümü).
- **Kapanış iddiası:** e-postalı satır varken bağlı satır 0 ise hata; boş veritabanı (CI, yeni
  geliştirme veritabanı — migration seed'den önce koşar) koşulsuz geçer. Ayrıca: bağlı her satırın
  digest'i kimliğinkine eşit, hiçbir kimlik iki backfill satırıyla bağlı değil.
- **Kilit:** tablo kilidi yok; aday satırlar `id` sırasıyla `FOR UPDATE` kilitlenir, ifadenin kendi
  `SET LOCAL lock_timeout = '1s'`'i altında. Bir satırı tutan bir yazarın arkasında 55P03 ile düşer —
  hiçbir şey yazılmaz, Job `Failed`, rollout yapılmaz, sakin bir anda yeniden koşulur. Kilitten sonra
  doğan satır (eski pod'un kaydı) M14-06b'nin (uzlaştırma, `00082`) işidir. İfade sürerken aday satırlarda
  girişin `last_login_at` UPDATE'i ve yeni oturum INSERT'inin yabancı anahtar denetimi (`FOR KEY SHARE`)
  bekler; panel istekleri (`TouchAdminSession` `admin_users`'ı yalnız okur) beklemez — canlıda ~8 ms.
- **Süre (M14-02 sayısıyla):** canlıda 4 `admin_users` satırı, 3 e-posta grubu (2 tekli, 1 çiftli) →
  **3 kimlik, 3 bağlı satır, 1 aktif owner satırı bağlanmamış** (çiftli grubun yeni olanı — contract ön
  koşulunun başlangıç değeri `05f` = 1). Ölçüm (bu dosyanın veritabanı, PostgreSQL 17, satırlar
  yazıldıktan sonra `ANALYZE` yok — en kötü istatistik), backfill ifadesi = aday satırların kilitli
  kaldığı süre: **4 satır 8 ms**, 3 000 satır (1 500 çift grup) 0,48 sn, 20 000 satır 3,9 sn —
  doğrusal, yazmalar baskın. Tek transaction, bölme yok: canlının boyutunda bölmenin kazancı yok ve
  kapanış iddiasını yazdığı satırlardan ayırırdı.
- **Digest:** `admin_users`'taki bcrypt CHECK'i `NOT VALID`'dir (`00018`), `admin_identities`'teki
  değil. Seçilen bir satırın digest'i bcrypt biçiminde değilse dosya **yalnız sayıyla** reddeder (INSERT'in
  kendi hatası başarısız satırı — e-postayı — Job log'una basardı, CLAUDE.md §7). Canlıda 0 (`08i`).
  Çözüm: o hesabın parolasını eski binary ile sıfırla, yeniden koş.
- **Eski binary:** bağlı satırlardaki parola değişimi / sıfırlama tüketimi rollout penceresinde
  tetikleyiciyle 500 alır (yukarıdaki adlandırılmış bedel); backfill satırların parolasına ve
  e-postasına dokunmaz, tetikleyici onu durdurmaz.

Sonrası (salt-okur, yalnız sayı; `tappa_owner`):

```sql
SELECT (SELECT count(*) FROM admin_identities)                               AS identities,
       (SELECT count(*) FROM admin_users WHERE linked_via = 'backfill')      AS backfill_links,
       (SELECT count(*) FROM admin_users
         WHERE email IS NOT NULL AND btrim(email::text) <> '' AND identity_id IS NULL) AS unlinked_with_email;
```

beklenen (2026-10-22 sayılarıyla): `3|3|1`.

**Down:** yalnız `linked_via='backfill'` satırlarını çözer ve yalnız onların kimliklerini siler. O
kimliklerden biri backfill'den sonra **yaşamışsa** (başka yolla bağlanmış bir satır, ret damgası,
doğrulanmış adres, onay penceresi, giriş, son kullanılan işletme ya da başka bir tablodan ona işaret eden
satır) hiçbir şeyi değiştirmeden **reddeder** — geri dönüş önceki binary'dir (ADR 0031 §8).

### Kimlik başına sıfırlama (M14-14) — `00075` ve canlı kimlik linklerinin acil iptali

- **`00075`** `resolve_identity_by_email` ve `identity_consume_reset`'i `DROP` + `CREATE` ile yeniden
  tanımlar (dönüşlerine kimliğin kayıtlı adresi eklenir — ADR 0031 §5.2'nin 2026-10-26 notu). Tablo
  kilidi yok; o an giriş yapan istek fonksiyonun kilidini bir transaction boyu bekler. Eski binary
  `resolve_identity_by_email`'i sütun adlarıyla okur ve yeni tanımda çalışır; `identity_consume_reset`'i
  çağırmaz. Yeni yetki yok. Down 00071/00072 tanımlarını bayt bayt geri koyar.
- **Bayrak bir kesme anahtarı DEĞİLDİR.** `TAPPA_ADMIN_IDENTITY=off` yalnız yeni kimlik linki basılmasını
  durdurur; basılmış bir kimlik linki ömrü (1 saat) boyunca **harcanabilir** — tüketim bayraktan
  bağımsızdır (ADR 0031 §8: yazma yolları kapılanmaz). Canlı kimlik linklerini hemen öldürmek gerekirse
  (sızmış bir posta kutusu, hatalı bir gönderim), `tappa_owner` ile. **Önce rol denetimi** —
  `admin_identity_resets` FORCE RLS altında ve politikasız; BYPASSRLS olmayan bir rol aşağıdaki sayımdan
  da UPDATE'ten de **sessizce 0** alır. Şu satır `t` dönmeli, dönmüyorsa durulur:

  ```sql
  SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user;
  ```

  sonra salt okuma sayımı:

  ```sql
  BEGIN READ ONLY;
  SELECT count(*) FROM admin_identity_resets WHERE used_at IS NULL AND cancelled_at IS NULL AND expires_at > now();
  ROLLBACK;
  ```

  sonra iptal — yüklemi sayımınkiyle **aynı** (süresi dolmuş linklere dokunmaz: onların "expired" izi
  "superseded"e dönmesin); dönen satır sayısı yukarıdakine yakın olmalı, çok farklıysa `ROLLBACK`:

  ```sql
  BEGIN;
  SET LOCAL lock_timeout = '1s';
  UPDATE admin_identity_resets SET cancelled_at = now() WHERE used_at IS NULL AND cancelled_at IS NULL AND expires_at > now();
  COMMIT;
  ```

  Yalnız `cancelled_at` değişir (geri çekmenin kendisi — `identity_withdraw_reset`'in yazdığı sütun);
  link satırı silinmez, kimliğe ve üyeliklere dokunulmaz. İptal edilen linki tıklayan kişi tek tip "That
  link no longer works" ekranını görür ve yeni bir link isteyebilir (bayrak açıksa kimlik linki, kapalıysa
  satır başı link). Satır başı linkler (`password_resets`) bu ifadenin kapsamında değildir.

### Adres kuralı (M14-08a) — `00076`

- **`00076`** `identity_create_for_signup`'ı aynı imzayla (`CREATE OR REPLACE`) yeniden tanımlar: kaydın
  adresi `mail.ValidRecipient`'ten geçmiyorsa (254 oktet, `=?` yok, yazdırılabilir ASCII'de net/mail'in
  kendine geri verdiği addr-spec) kimlik doğmaz ve satır bağlanmaz; adresin e-posta-geniş okuması
  yazdırılabilir ASCII olmayan bir satırı artık saymaz (bir homoglif satırı gerçek adresin kimliğini
  engellemesin). Tablo kilidi yok, yeni yetki yok; eski binary aynı çağrıyı yapar ve ASCII olmayan
  adreste `false` alır (satır bağlanmamış doğar — bilinen bir adres gibi). `admin_identities.email`'e
  CHECK **eklenmedi**: ölçüldü, ihlalin `DETAIL`'i adresi ve digest'i hataya ve sunucu log'una basar
  (00076'nın başlığı). Down 00074'ün gövdesini bayt bayt geri koyar.
- Kayıt formu aynı kuralı sınırda uygular (`signup.ValidateAccount`); giriş ve sıfırlama formları yalnız
  arar, değişmedi. Backfill'in tarafı yukarıdaki ikinci ön kontroldür (Expand deploy'undan önce).
- **Up'ın kendi iddiası:** koşan rol superuser ya da BYPASSRLS değilse, ya da yazdırılabilir ASCII
  olmayan adresli tek bir kimlik varsa (`00073` onu önceki ön kontrolün görmediği bir satırdan doğurmuş
  olabilir), dosya hiçbir şey değiştirmeden **yalnız sayıyla** hata verir.
- **Migrate'ten sonra** (salt okuma, yalnız sayı; `tappa_owner`):

  ```sql
  BEGIN READ ONLY;
  SELECT count(*)                                                AS identities,
         count(*) FILTER (WHERE email::text !~ '^[\x21-\x7e]+$') AS not_printable_ascii,
         count(*) FILTER (WHERE NOT (octet_length(email::text) <= 254
                              AND strpos(email::text, '=?') = 0
                              AND email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$')) AS failing_send_rule
    FROM admin_identities;
  ROLLBACK;
  ```

  beklenen: `not_printable_ascii` = 0 (00076 bunu garanti eder) ve `failing_send_rule` = ön kontrolün
  üçüncü sayımı eksi ikincisi (backfill'in ASCII ama biçimi bozuk kalıntısı; 2026-10-22 sayılarıyla
  `3|0|0`; bu dalın veritabanında 2026-10-27: `0|0|0`).

### Uzlaştırma (M14-06b) — `00082` (Deploy 2)

`identity_reconcile()` bir **bakım fonksiyonudur** (ADR 0031 §5.2, §5.8; CLAUDE.md §4.5 (3)): backfill'den
**sonra** doğan `admin_users` satırlarına (expand rollout penceresinde eski pod'un kayıtları, expand ile
M14-17 arasındaki her kayıt) backfill'in **aynı** kuralıyla kimlik verir — kimliği henüz olmayan her dolu
e-posta grubunun **en eski aktif** satırı (yoksa en eskisi) bağlanır (`linked_via='reconcile'`,
`linked_at=now()`), kimlik o satırın digest'iyle doğar, öbür satırlar bağlanmamış kalır. Adresinde zaten
bir kimlik olan grup ve bağlı ya da reddedilmiş (`unlinked_at`) satır taşıyan grup **dokunulmadan** kalır:
sonradan doğan bir satır, adresin önceki bir satırının doğurduğu kimliğe asla katılmaz (M14-06 güvenlik
notu); reddedilmiş satır hiçbir koşuda bağlanmaz. Davet kabulünün (`00077`, `invite`) ve kaydın onayının
(`00078`/`00080`, `claim`) yazdığı satırlar **bağlı doğar**: aday değildir, grupları da dokunulmadan kalır.
İdempotenttir: ikinci koşu 0 satır bağlar. Sahibi `tappa_identitydefiner`; `EXECUTE` **yalnız
`tappa_owner`'da** — `tappa_app` (ve operatör rolleri) onu çağıramaz, uygulama yolu yoktur. Dönüş değeri: o
çağrıda bağlanan satır sayısı. Yeni yetki yok: gövdenin `00071`/`00072`'nin ötesinde kullandığı iki hücre
(`admin_users` `SELECT (password_hash)`, `admin_identities` `INSERT (email, password_hash)`) `00074`'ündür.

- **Adres kuralı (M14-08a — ADR 0031 §5.8'in notu):** grubun adayları yalnız adresi `mail.ValidRecipient`'ten
  geçen satırlardır (00076'nın 6b yüklemi, desen bayt bayt). Homoglif satırı (U+0130 `İ`, U+212A KELVIN
  SIGN — citext onları ASCII harfe katlar) grubun **en eski aktif** satırı olsa bile **seçilmez**: kimlik
  grubun kuralı geçen satırının yazımı ve digest'iyle doğar, homoglif satırı bağlanmamış kalır. Yalnız
  kuralı geçmeyen satırlardan oluşan grup (karşılığı olmayan homoglif; ASCII ama biçimi bozuk `a..b@x.mt`)
  kimlik **doğurmaz** — adlandırılmış kalıntıdır; aşağıdaki M14-19 sayımı onu ayrı sayar, ne olacağı
  kullanıcının kararıdır (ADR 0031 §5.8).
- **Deploy 2 (ADR 0031 §8, adım 3)** — Deploy 1'den (expand; canlıda 2026-10-28, goose 81, bayrak `off`)
  **sonraki ayrı** deploy. Binary'nin işi yok: `00082`'nin migrate Job'ı (rollout'tan önce) fonksiyonu
  yaratır ve **bir kez çağırır**; ayrı bir adım yok, bayrak `off` kalır. `main`'e birleştirme deploy
  kararıdır (kullanıcının). Dosya başta koşan rolün superuser ya da BYPASSRLS olduğunu, `00076`'nın adres
  kuralının ve `00074`'ün iki yetkisinin yerinde olduğunu doğrular; sonda (kapanış iddiası): bu
  transaction'dan önce doğmuş, **kuralı geçen** satırlar arasında kuralın alması gereken **hiçbir grup**
  kimliksiz kalmamış (definer BYPASSRLS'ini yitirmişse fonksiyon 0 satır görür — dosya o zaman "başarılı"
  kaydedilmez, reddeder; boş veritabanı koşulsuz geçer), `reconcile` ile bağlı her satırın digest'i
  kimliğinkine eşit, hiçbir kimlik iki `reconcile` satırıyla bağlı değil, `reconcile` ile bağlı hiçbir
  kimliğin adresi kuralı çiğnemiyor, fonksiyonun katalog biçimi (definer, `search_path`, `EXECUTE` yalnız
  `tappa_owner`) doğru.
  - **Önce (salt okuma, yalnız sayı; kullanıcı koşar):** aşağıdaki "M14-19'un ön-kontrol sayımları"
    sorgusu, olduğu gibi. 2026-10-28 ölçümüyle (Deploy 1'den sonra, salt okuma: 3 kimlik, 3 bağlı satır,
    kimliği olmayan 1 dolu e-postalı satır — M14-06'nın beklenen tek bağlanmamış aktif owner'ı, çift
    grubun ikinci satırı; adresinde kimlik var; adres sayımları `4|0|0`) beklenen `BEGIN` · `0|0` · `0` ·
    `ROLLBACK`. Birinci sayı 0'dan büyükse Deploy 2'nin çağrısı tam o kadar grubu bağlayacaktır (Deploy
    1'den beri `identity_create_for_signup`'ın bağlamadığı kayıtlar — eski pod'un rollout penceresi).
  - **Sonra:** goose sürümü `82`; Job log'unda `OK 00082_reconcile_admin_identities.sql`. Aynı sayım
    yeniden: birinci sayı `0` (çağrı onları bağladı), ikincisi öncekinden **büyük değil** (bu çağrıda
    ASCII ikizi kimliğini alan bir homoglif satırı kalıntıdan düşer; 2026-10-28 olgularıyla `0` → `0`), üçüncüsü `0`. Job `Failed`
    ise hiçbir şey yazılmamıştır (dosya tek transaction): 55P03 → sakin bir anda yeniden; kapanış
    iddiası ya da digest reddi → **DUR**, rollout yok, mesaj satır ya da adres anmaz.
- **Canlı uygulamayla eşzamanlılık (T8):** her grup için önce adresin ad alanlı danışma kilidi
  (`hashtextextended('admin-identity-email:' || lower(email), 0)` — her kimlik INSERT'inin aldığı anahtar;
  citext eşitliği `lower()`'ınki olduğu için bir homoglif ve katlandığı ASCII yazım aynı anahtarı paylaşır),
  sonra grubun aday satırları `id` sırasıyla `FOR UPDATE` (kuralı geçenlerin kimlikleri tutulur);
  bağlı/reddedilmiş grup kontrolü bu kilitlerden **sonra** okunur (danışma kilidini almayan bir bağlayıcının
  — `identity_link_legacy` — arada commit ettiği bağ görülür); seçim **yalnız kilitlenen, kuralı geçen
  satırlar** arasından yapılır (bekleme sırasında doğan satır kilitsiz seçilmez, kimlik bayat bir digest'le
  doğmaz); kimlik INSERT'i `ON CONFLICT (email) DO NOTHING` — aynı adresin eşzamanlı bir kaydı ya da ikinci
  bir uzlaştırma **500 vermez**, grup atlanır. Tablo kilidi yok; `SET LOCAL lock_timeout = '1s'` altında bir
  satırı tutan yazarın arkasında 55P03 ile düşer — hiçbir şey yazılmaz, Job `Failed`, sakin bir anda yeniden
  koşulur.
- **Digest:** seçilen satırın digest'i bcrypt biçiminde değilse çağrı **hiçbir şey yazmadan** reddeder;
  mesaj satırı ve adresi anmaz (CLAUDE.md §7). Çözüm backfill'deki gibi: o hesabın parolasını sıfırla,
  yeniden koş. (`00018`'den sonra doğan satırda imkânsız — CHECK yeni satırı denetler.)
- **Binary'yi expand'in altına geri almak yalnız uzlaştırmayla birlikte** (ADR 0031 §5.8): eski binary
  geri dönüp yeniden ileri alınırsa, aradaki kayıtlar için aşağıdaki yeniden çağrı koşulur.

**M14-19'dan hemen önce: yeniden çağrı** (kullanıcı koşar; `tappa_owner`; sır yok — parola pod'un kendi
env'inden):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN;
SET LOCAL lock_timeout = '1s';
-- 00082'nin rol iddiası, bayt bayt: superuser ya da BYPASSRLS olmayan bir rol FORCE RLS altında aşağıdaki
-- denetimlerde 0 satır görür ve sessizce geçerdi.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b): the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it could not check what the reconciliation left (ADR 0031 5.8). Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
SELECT public.identity_reconcile();
-- 00082'nin kapanış iddiası (b), (c) ve (e), bayt bayt, COMMIT'ten ÖNCE: biri tutmazsa hata -> ON_ERROR_STOP ->
-- hiçbir şey commit edilmez (TestIdentityReconcile_TheRunbookRecallRepeatsTheFilesChecks bunları dosyayla eşler).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.linked_via = 'reconcile' AND a.identity_id IS NOT NULL
                  AND a.password_hash IS DISTINCT FROM
                      (SELECT i.password_hash FROM public.admin_identities i WHERE i.id = a.identity_id)) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: a row linked by the reconciliation does not carry its identity''s password digest (ADR 0031 5.1). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users
                WHERE linked_via = 'reconcile' AND identity_id IS NOT NULL
                GROUP BY identity_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: an identity is linked through more than one reconcile row; the reconciliation links one row per e-mail group (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users a
                 JOIN public.admin_identities i ON i.id = a.identity_id
                WHERE a.linked_via = 'reconcile'
                  AND (pg_catalog.octet_length(i.email::text) <= 254
                       AND pg_catalog.strpos(i.email::text, '=?') = 0
                       AND i.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: an identity the reconciliation linked carries an address mail.ValidRecipient refuses (M14-08a, ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
COMMIT;
SQL
```

beklenen: `BEGIN` · `SET` · `DO` · bir sayı (bu çağrının bağladığı satır — Deploy 2'den beri doğmuş
kimliksiz grup sayısı; sıfır da olabilir) · `DO` · `COMMIT`. Bir `DO` hata verirse psql durur ve transaction
commit edilmez — DUR, bayrak açılmaz (mesaj satır ya da adres anmaz). 55P03 → sakin bir anda yeniden. Başka bir hata → DUR, bayrak açılmaz.

**M14-19'un ön-kontrol sayımları** (bu kartın runbook adımı; yeniden çağrıdan **sonra**; salt okuma, yalnız
sayı, `READ ONLY` transaction ve `ROLLBACK`). Birinci sorgu iki sayı basar: kuralı geçen kimliksiz dolu
e-posta adresleri ve — **ayrı**, adlandırılmış kalıntı olarak — kuralı (`mail.ValidRecipient`, 00076'nın
6b deseni, bayt bayt) geçmeyen kimliksiz satırlar:

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN TRANSACTION READ ONLY;
-- 1. Kimliği olmayan dolu e-posta (reddedilmiş satırlar hariç): adreste kimlik yok. Kuralı geçen adresler
--    (DUR sayısı) ve kuralı geçmeyen satırlar (adlandırılmış kalıntı, ayrı).
WITH identityless AS (
    SELECT a.email,
           (NOT (octet_length(email::text) <= 254
                 AND strpos(email::text, '=?') = 0
                 AND email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$')) AS residue
      FROM admin_users a
     WHERE a.email IS NOT NULL AND btrim(a.email::text) <> ''
       AND a.unlinked_at IS NULL
       AND NOT EXISTS (SELECT 1 FROM admin_identities i WHERE i.email = a.email))
SELECT count(DISTINCT email) FILTER (WHERE NOT residue) AS identityless_filled_email,
       count(*) FILTER (WHERE residue)                  AS identityless_rule_residue
  FROM identityless;
-- 2. Bağlı satırda digest uyuşmazlığı (dual-write değişmezi, ADR 0031 §5.1).
SELECT count(*) AS linked_digest_mismatch
  FROM admin_users a
  JOIN admin_identities i ON i.id = a.identity_id
 WHERE a.password_hash IS DISTINCT FROM i.password_hash;
ROLLBACK;
SQL
```

beklenen: `BEGIN` · `0|N` · `0` · `ROLLBACK` — `N` kalıntıdır (2026-10-28 ölçümüyle `0`). `identityless_filled_email`
ya da `linked_digest_mismatch` 0 değilse **bayrak açılmaz** (M14-19 durur): birincisi için yeniden çağrı bir kez
daha koşulur (araya bir kayıt girmiş olabilir); hâlâ 0 değilse grup ya legacy digest taşıyor (çağrı zaten
reddederdi) ya da bağlı/reddedilmiş bir satırı olup adresi kimliksiz — inceleme, kart dışı. Digest
uyuşmazlığı her zaman bir hatadır. `N > 0` bayrağı **durdurmaz**: o satırlar hiçbir yoldan bağlanmaz ve
kimlik doğurmaz (homoglif ya da biçimi bozuk adres); sayı deploy'dan önce kullanıcıya bildirilir, K-3'ün
son tarihi ve contract A'nın ön koşulu karşısında ne olacakları kullanıcının kararıdır (ADR 0031 §5.8).
(`count(identity_id IS NOT NULL AND unlinked_at IS NOT NULL) = 0` — S19 — M14-19'un kendi üçüncü sayımıdır:
aşağıdaki "M14-19 — bayrak" bölümünün (a) 2. adımı.)

**Down:** `00073`'ünkünün aynası, `linked_via='reconcile'` için: o satırları çözer ve yalnız onların
kimliklerini siler (bir `reconcile` bağı daima aynı çağrının doğurduğu kimliğedir); kimliklerden biri
**yaşamışsa** (başka bir bağ — davetle ya da kayıt onayıyla dahil —, ret damgası, doğrulanmış adres, onay
penceresi, giriş, son kullanılan işletme, ona işaret eden başka bir tablo satırı) hiçbir şeyi değiştirmeden
**reddeder**. Sonra fonksiyonu düşürür; yetki geri almaz (`00082` yetki vermedi; kullandığı iki hücre
`00074`'ündür, onun Down'ı alır). Canlıda koşulmaz — geri dönüş önceki binary'dir (ADR 0031 §8). Contract B
(M14-20b) fonksiyonu düşürür.

### M14-19 — bayrak: `TAPPA_ADMIN_IDENTITY=on` ve `00083`

Bayrağı açmak **bir `main` birleştirmesidir = bir deploy = kullanıcının kararı** (ADR 0031 §8). Dal
`m14-flag` migration commit'ini (`00083`, aşağıda) ve ayrı bir commit'i — `feat(deploy): turn the admin
identity on` — taşır; ikincisi yalnız `deploy/k8s/05-config.yaml`'ın `TAPPA_ADMIN_IDENTITY` satırını `"on"`
yapar (ve onu sabitleyen paketleme testini, `TestPackaging_TheIdentityFlagShipsOn`). **İkisi tek
birleştirmede çıkar** (aşağıdaki boşluk (ii); kullanıcı kararı 2026-10-28). Sandbox da `"on"`dur (ayrı
commit `feat(deploy): turn the admin identity on in the sandbox`, aynı karar). `kubectl edit` **yasak**:
bir sonraki deploy dosyayı yeniden uygular ve değeri sessizce geri çevirir.

**`00083` ne yapar** (tek dosya, tek transaction, `tappa_owner`; deterministik ve testli —
`internal/db/adminflag_test.go`):

1. **Onay pencereleri (S5, T9).** `confirm_opened_at IS NOT NULL` olan **her** kimliğin yalnız
   `confirm_open_until`'i `GREATEST(confirm_open_until, now() + 7 gün)` olur; `confirm_opened_at` ve
   `confirm_scope` **değişmez** (reset listesi `linked_at < confirm_opened_at`'tir). **Neden bu yüklem:**
   bayrak şemada kayıtlı değil (pod env'i); şemanın bildiği tek şey bir pencerenin **var** olduğudur
   (`00069`'un `admin_identities_confirm_window_whole` CHECK'i: üç sütun birlikte NULL). Bu dosyaya kadar
   bayrak hiçbir veritabanında açılmadı ve kapalıyken bir pencereyi saatinden başka hiçbir şey kapatamaz
   ("Keep all", çözme, geri alma bayrağın kapattığı yüzeylerdir) — yani dosyanın gördüğü her pencere
   bayrak kapalıyken açılmıştır; süresi **dolmuş** olanlar da dahil (kişi listeyi hiç görmedi: ADR'nin
   `max(olay, bayrak günü) + 7`'si). "Keep all" ile kapanmış pencerenin sütunu yoktur, dokunulmaz.
   `GREATEST`: bu transaction başladıktan sonra commit eden bir açılış zaten `now() + 7`'den saniyeler
   sonra biter; kısaltılmaz.
2. **`REVOKE EXECUTE ON FUNCTION identity_create_for_signup FROM tappa_app` (S18).** M14-17'den beri hiçbir
   binary onu çağırmıyor: bu dalın kaynağı ve `main` `8d94a95e`'nin derlenmiş `cmd/tappa`, `cmd/opadmin`
   ve sandbox ikilileri adı taşımıyor (M14-19'da ölçüldü; kontrol olarak `identity_claim_signup`
   ikilide bulundu) ve `TestIdentityCreateForSignup_NoBinaryCallsIt` modülün test dışı her dosyasını
   tarar — rollout penceresindeki eski pod da onu çağırmaz, expand/contract kuralı tutar. Fonksiyon
   **kalır**; `DROP` contract B'nin (M14-20b).

Başta rol iddiası (superuser ya da BYPASSRLS değilse FORCE RLS altında 0 pencere görür → **reddeder**),
sonda postcondition (bayrak günü + 7'den önce biten pencere kalmadı; fonksiyon sahibinden başka hiçbir
rolce çağrılamaz ve `00076`'nın bıraktığı biçimde). `lock_timeout = '1s'`: pencereli kimlik satırlarını
tutan canlı bir yazar varsa 55P03 → hiçbir şey yazılmaz, Job `Failed`, sakin bir anda yeniden. Tablo
kilidi yok. **Down** yalnız yetkiyi geri verir; pencerelerin eski sonları hiçbir yerde tutulmadı ve
bayrak kapalıyken daha uzun bir pencere görünmez — canlıda koşulmaz, geri dönüş aşağıdaki (f)'dir.

🔴 **İki adlandırılmış boşluk.** (i) Aynı deploy'un rollout'unda eski pod (bayrak kapalı) Job'dan sonraki
birkaç dakikada bir pencere açabilir; o pencere kendi olayı + 7'de biter — bayrak günü + 7'den dakikalar
kısa, kabul. (ii) Migration bayrak commit'inden **önceki** bir deploy'la çıkarsa iki şey olur: iki deploy
arasında açılan her pencere olay + 7'de kalır, **ve** `00083`'ün o gün taşıdığı pencereler de *o günün*
+ 7'sinde biter — bayrak deploy'u 7 günden geç gelirse **hepsi** bayrak onları gösteremeden kapanmış olur.
Ölçüsü (d)'nin üçüncü sorgusudur: `windows_ending_early > 0`. **Karar (kullanıcı, 2026-10-28): iki commit
tek birleştirmede, tek deploy'da.** (Migration bayrak kapalıyken yine de güvenlidir —
`TestPanelIdentityFlagOff_AWindowMovedBy00083ChangesNothing`: taşınmış bir pencereyle bile kabuk, hesap
sayfası ve `/admin/switch` bağsız oturumunkiyle bayt-aynı, "Keep all" 404.) (iii) Bir `off → on` döngüsü
`00083`'ü yeniden koşturmaz: geri dönüşten sonraki ikinci açılışta, aradaki kapalı günlerde açılan
pencereler **taşınmaz**. Çaresi yeni bir migration olurdu — CLAUDE.md §4.5 (3)'ün adlandırılmış bakım
yazarlarına bir yenisini eklemek demektir; o gün **kullanıcının kararıdır**, bu kart onu kurmaz.

**(a) Ön kontroller** (salt okuma, yalnız sayı; kullanıcı koşar; birleştirmeden **hemen önce**):

1. Yukarıdaki *"M14-19'un ön-kontrol sayımları"* (Uzlaştırma bölümü), **(b)'deki yeniden çağrıdan
   sonra**, olduğu gibi: beklenen `BEGIN` · `0|N` · `0` · `ROLLBACK`. `identityless_filled_email` ya da
   `linked_digest_mismatch` 0 değilse **DUR**, bayrak açılmaz; `N` (kalıntı) durdurmaz, kullanıcıya bildirilir.
2. S19 ve pencerelerin bugünkü sayısı:

   ```bash
   kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
       sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
   BEGIN TRANSACTION READ ONLY;
   -- S19: bağlı ama ret damgalı satır. Çözme (identity_unlink_backfilled) bağı kaldırır ve damgalar; ikisi
   -- birlikte bir tutarsızlıktır.
   SELECT count(*) AS linked_but_refused
     FROM admin_users
    WHERE identity_id IS NOT NULL AND unlinked_at IS NOT NULL;
   -- 00083'ün taşıyacağı pencereler: hepsi | süresi dolmuş olanlar.
   SELECT count(*) AS windows_to_move,
          count(*) FILTER (WHERE confirm_open_until <= now()) AS windows_expired
     FROM admin_identities
    WHERE confirm_opened_at IS NOT NULL;
   ROLLBACK;
   SQL
   ```

   beklenen: `BEGIN` · `0` · `W|E` · `ROLLBACK`. `linked_but_refused` 0 değilse **DUR** (inceleme, kart
   dışı). `W` ve `E` yalnız kayıt içindir (deploy sonrası karşılaştırma). Bayrak kapalıyken pencere açan
   yollar koddan okunduğu kadarıyla ikidir: bağlı bir satırın satır başı sıfırlama linkinin tüketimi
   (`identity_consume_row_reset`) ve var olan bir adresle kayıt onayı (`identity_claim_signup`).

**(b) Uzlaştırmanın yeniden çağrısı** — yukarıdaki *"M14-19'dan hemen önce: yeniden çağrı"* bloğu, olduğu
gibi. ⚠️ Bu **canlı bir YAZMADIR** (kimlik doğurur, satır bağlar); kullanıcı koşar, `tappa_owner`.
Beklenen çıktı ve DUR koşulları orada yazılı. Sonra (a)'yı koş.

**(c) Birleştirme = deploy.** Yol: `m14-flag` → `m14-identity` (birleştirme) → PR → `main` (CI yeşil);
`m14-identity` bu deploy'a T178 (kuşağın aileye bölünmesi) ve T181'i (backlog) da taşır. `deploy.yml`
sırası: `05-config.yaml` uygulanır (808), migrate Job `00083`'ü koşar (888), sonra rollout (934) — yeni
pod env'i başlarken okur ve bayrak `on` ile kalkar. Eski pod rollout bitene dek `off`'tur (yukarıdaki
boşluk (i)).

🔴 **Migrate Job `Failed` ise (ör. 55P03) ConfigMap ZATEN `on`'dur** — şema 82'de kalır ve o an yeniden
başlayan eski pod bayrak açık kalkar (kimlik yüzeyleri 00083'süz şemada: pencereler taşınmamış). **Hemen,
herhangi bir pod yeniden başlamadan önce:** iş akışını sakin bir anda yeniden koştur (Job tek
transaction'dır, hiçbir şey yazmamıştır) **ya da** `main`'in önceki `05-config.yaml`'ını uygula
(`git show <önceki main>:deploy/k8s/05-config.yaml | kubectl --context hetzner-k8s-1 apply -f -` —
asla `kubectl edit`); sonra pod'un açılış satırındaki `"admin_identity"` değerini oku
(`kubectl -n tappa logs deployment/tappa | grep '"admin_identity"'`). Kalıcı çözüm — ConfigMap'i Job'dan
sonra uygulamak — `deploy.yml`'in değişmesi demektir ve bu kartın kapsamı dışındadır.

**(d) Deploy'dan sonra** (salt okuma; Job log'unda `OK   00083_prepare_the_admin_identity_flag.sql`;
pod'un açılış satırı `"admin_identity":"on"`):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN TRANSACTION READ ONLY;
SELECT max(version_id) AS goose_version FROM goose_db_version WHERE is_applied;
SELECT has_function_privilege('tappa_app', 'public.identity_create_for_signup(uuid,uuid)', 'EXECUTE') AS app_calls_signup_door,
       has_function_privilege('tappa_identitydefiner', 'public.identity_create_for_signup(uuid,uuid)', 'EXECUTE') AS owner_keeps_it;
-- Deploy'dan sonraki ilk saat içinde koşulur: o saatte bayrak günü + 7'den bir saatten fazla erken biten
-- pencere olmamalı (eski pod'un rollout dakikalarında açtığı bile en çok dakikalarca erkendir).
SELECT count(*) AS windows,
       count(*) FILTER (WHERE confirm_open_until < now() + interval '7 days' - interval '1 hour') AS windows_ending_early
  FROM admin_identities
 WHERE confirm_opened_at IS NOT NULL;
ROLLBACK;
SQL
```

beklenen: `BEGIN` · `83` · `f|t` · `W'|0` (`W'` ≥ (a)'daki `W`) · `ROLLBACK`. `app_calls_signup_door = t`
ya da `windows_ending_early > 0` → migration'ın koştuğu sürüm yanlış; incele (geri dönüş gerekmez, bayrak
güvenlik açığı doğurmaz). **K-3 saati:** bu deploy'un günü (UTC) — orkestratör `state.md`'ye tarihiyle
yazar; contract A en erken bu tarih + 60 gün.

**(e) Duman** (`https://taptime.mt/admin`, kullanıcının kendi test hesaplarıyla — aynı e-postanın KF ve
KM'de birer owner üyeliği, ikisi bağlı; davet için kendi posta kutusu):

1. **Giriş (kimlik yolu).** Çıkış yap → e-posta + parola → son kullanılan işletmeye iner (K-4), seçici
   yok; kabukta işletmenin adı ve **"Switch business"** bağlantısı var.
2. **KF ↔ KM geçişi.** "Switch business" → liste iki işletmeyi gösterir → KM → kabuk KM'yi gösterir;
   tekrar KF'ye. Her iki işletmenin denetim kaydında geçiş sıradan bir çıkış + giriş satırıdır (karşı
   işletmenin adı yok).
3. **Eski sekme 409.** KF'de bir formu (ör. hesap sayfası) bir sekmede açık bırak; başka sekmede KM'ye
   geç; eski sekmede formu gönder → **409** *"This page belongs to another business — reload it."*,
   hiçbir şey yazılmaz; sayfayı yenilemek KM'nin formunu getirir.
4. **Sign out everywhere.** İkinci bir tarayıcıda (gizli pencere) da gir; ilkinde Hesap → **"Sign out
   everywhere"** → ikinci tarayıcının bir sonraki tıklaması `/admin/login`'e döner; iki işletmenin
   oturumları da biter.
5. **Yanlış parola.** Doğru e-posta + yanlış parola → genel hata (adresin varlığını söylemez); doğru
   parolayla giriş sürer. (Art arda denemeler oran sınırına takılır — bunu canlıda zorlamayın.)
6. **Davet.** Owner olarak Managers → kendi kontrolündeki **yeni** bir adrese davet → e-posta gelir →
   link → yeni parola → davetli girer ve yalnız o işletmeyi görür. Temizlik: davetli üyeliği devre dışı
   bırak (aynı sayfanın **"People who run this business"** bölümü). Var olan bir kimliğe davet aynı
   e-postalı kimliğin canlı oturumuyla kabul edilir.
7. 🔴 **ZORUNLU, açılıştan hemen sonra: "Link a business".** Canlıda bağlanmamış **bir** aktif owner satırı
   var: adresinde zaten bir kimlik olan grubun ikinci satırı (M14-06'nın beklenen tek satırı — (a)'nın
   kimliksiz sayımı onu **saymaz**, çünkü adresin kimliği var). O kişi kimlik yoluyla girer ama o satır
   kimliğe bağlı değildir: **"Sign out everywhere"** onun oturumlarını **iptal etmez** (yalnız kimliğe bağlı
   satırları kapsar, sayfa "every business you belong to" dese de) ve kimlik parolası değişikliği o satıra
   **ulaşmaz**. Kişi Hesap → **"Link a business"**'te kendi parolasını ve o işletmenin (eski) parolasını
   girer → satır bağlanır (yerel provada ölçüldü, aşağıda (f)); sonra (a)'nın S19 sorgusu ve haftalık
   sayımın `unlinked_active_rows`'u bir azalmış olmalı. Choose paragrafı onu oraya yönlendirir.
   **M14-22'den sonra:** iki parolalı form kalktı; aynı satır panelin bandından e-postayla bağlanır — aşağıdaki
   *"M14-22 — e-postayla bağlama"* bölümünün dumanı.

Herhangi bir adım 5xx verirse ya da beklenenden saparsa → (f).

**(f) Geri dönüş = `off` commit'i + deploy** (kullanıcı kararı). Yeni bir dalda bayrak commit'ini geri al
(`git revert <bayrak commit'i>` — `05-config.yaml` `"off"`, paketleme testi geri döner), PR, CI, `main`.
Migration geri **alınmaz**: binary aynı, şema `83` kalır ve eski kol bu şemada bugünkü gibi çalışır
(`00083` yalnız kimsenin çağırmadığı bir yetkiyi aldı ve pencere sonlarını uzattı; bayrak kapalıyken
pencere görünmez — `TestPanelIdentityFlagOff_AWindowMovedBy00083ChangesNothing`). **Ne kalır:** kimlikler,
bütün bağlar (açıkken kurulan `password` / `account` / `invite` / `claim` bağları dahil — dual-write
parolaları satırlarda doğru tutar, eski kol bağlı satıra kimliğin digest'iyle girer), pencereler olduğu
gibi (kapalıyken görünmez, saatleri işler), reddedilmiş satırlar (nöbetçi digest: eski kolla da girilemez),
geçişle açılmış oturumlar (sıradan, tek işletmeli oturumlardır; yaşar), bekleyen davetler (kabul 404 —
süreleri dolarsa yeniden açılışta yeniden davet), basılmış kimlik sıfırlama linkleri (ömürleri boyunca
harcanabilir — bayrak kesme anahtarı değildir, yukarıdaki M14-14 bölümü). **Kapanan:** geçiş, hesap
sayfasının kimlik bölümleri, davet, üyelik yönetimi, kimlik başına sıfırlama talebi (satır başına döner),
kimlik yolu girişi (herkes eski yoldan). **Yeniden açtıktan sonra** (d)'nin üçüncü sorgusunu koş, ama
`WHERE`'ine `AND confirm_opened_at >= '<off deploy'unun anı>'` ekleyerek (ilk açılışta taşınmış pencereler
o gün + 7'de biter; daraltmasız sorgu onları da sayar): `windows_ending_early > 0` boşluk (iii)'tür —
kapalı günlerde açılmış, taşınmamış pencereler (sayı yalnız bildirilir; çaresi kullanıcının kararı).

**Prova: 2026-10-28, yerel** (kullanıcı kararı: *"Yerelde, birleştirmeden önce"*). Dalın gerçek ikilisi
(`go build ./cmd/tappa`, `m14-flag` HEAD), `TAPPA_ENV=dev`, lane veritabanı goose `83`; aynı ikili üç kez
başlatıldı: `on` → `off` → `on` (açılış satırı her seferinde `"admin_identity":"on"/"off"/"on"`). Durum:
bir kişi (KF ve KM'de bağlı owner satırı, bir kimlik), aynı adresin **bağlanmamış** üçüncü satırı (başka
bir işletmede, başka parola — canlıdaki M14-06 satırının şekli), bağlanmamış bir manager (KF), açık bir
`reset` penceresi, bekleyen bir yönetici daveti (`AdminInvites.Issue`) ve bekleyen bir kayıt taslağı
(`SignupClaims.Open`). HTTP kodları `curl` ile ölçüldü:

| Faz | Adım | Gözlenen |
|---|---|---|
| on | kişinin girişi | `303 /admin`, `path=identity`, son kullanılan yok → en eski üyelik (KF) |
| on | `GET /admin/switch` · KM'ye geçiş | `200` · `303 /admin`; KF oturumu iptal, KM oturumu canlı |
| on | eski KF sekmesinden form | `409` *"This page belongs to another business — reload it."*, satır değişmedi |
| on | hesap sayfası (pencere açık) · davet linki · kayıt linki/sayfası | "Keep all" görünür · `303 /admin/invite` · `303` / `200` |
| on | bağlanmamış manager'ın girişi | `303 /admin`, `path=legacy` |
| off | kimlik yolunda açılmış KM oturumu | `GET /admin` `200` (yaşar) |
| off | `/admin/switch` · keep-all POST · davet linki | `404` · `404` · `404` |
| off | hesap sayfası | "Keep all" / "Your businesses" / "Sign out everywhere" yok |
| off | kayıt linki/sayfası | `303` / `200` (iki durumda da açık) |
| off | kişinin yeni girişi (eski kol) | `303 /admin/login/choose`, seçici KF ve KM'yi sunar (üçüncü satır o parolayla doğrulanmaz); KF seçildi → `303 /admin`, `path=legacy` |
| off | bağlanmamış üçüncü satır, kendi parolasıyla | `303 /admin`, `path=legacy` |
| off | bağlı satırda parola değişikliği | `303 …?done=password`; kimlik ve iki bağlı satır aynı yeni digest, kişinin öbür oturumu iptal (eski KM oturumu → `303 /admin/login`) |
| off | bağlanmamış iki satırda parola değişikliği | ikisi de `303 …?done=password`; satır kendi digest'ini yazar, bağsız kalır |
| off | bağlı satıra doğrudan yazım (sahip rolüyle) | tetikleyici reddeder (*"written through the identity alone"*) |
| on | eski kolla açılmış oturumlar (manager, kişinin KF'si) | `GET /admin` `200`; kişininkiyle `/admin/switch` `200` |
| on | kişinin girişi, yeni parola | `303 /admin`, `path=identity`, KM'ye iner (geçişin yazdığı son kullanılan işletme — K-4) |
| on | hesap sayfası · davet linki · kayıt linki/sayfası | "Keep all", "Your businesses", "Link a business" görünür · `303` · `303` / `200` |
| on | "Link a business" (kendi + üçüncü satırın parolası) | `303 …?done=linked`; satır bağlandı, digest kimliğinkine eşit |
| on | "Sign out everywhere" | `303 /admin/login`; kişinin **üç** satırında canlı oturum `0`, manager'ınki yaşar |

Sayımlar (salt okuma, fazlar arasında): `goose=83`, `digest_mismatch=0` üç fazda da; bekleyen davet `1` ve
bekleyen taslak `1` üç fazda da kaldı; pencere `reset`/açık üç fazda da kaldı (kapalıyken görünmedi,
kapanmadı). İkinci `on`'dan sonra (d)'nin üçüncü sorgusu `1|1` verdi: provanın kendi penceresi `00083`'ten
**sonra** (bir gün geriye tarihlenerek) açılmıştı — boşluk (iii)'ün tam şekli, sorgu onu görüyor. Hata
satırı yalnız ilk fazda iki tane: provanın kendi uydurduğu geçersiz bir taslak (`500`, düzeltilip yeniden
açıldı). Prova verisi lane veritabanından silindi. **Canlıda prova yapılmadı**; canlıda geri dönüş
gerekirse yukarıdaki adımlar aynıdır.

**(g) İzleme — e-postasız sayaçlar** (ADR 0031 §5.15; contract'ın ölçüsü). Hepsi `TAPPA_LOG_FORMAT=json`
log satırlarından hesaplanır; hiçbiri adres, `identity_id` ya da token taşımaz (alanlar `admin_user_id` /
`actor_id`, `ip`, `path`, `linked`, `outcome`):

| Sayaç | Olay (`msg`) ve filtre | Kaynak |
|---|---|---|
| kimlik yoluyla giriş | `panel sign-in`, `path = "identity"` | `adminloginidentity.go` `completePersonLogin` |
| eski kolla giriş | `panel sign-in`, `path = "legacy"` (bu dalla eklendi; önceki binary'lerin satırında `path` yok) | `adminlogin.go` `completeLogin` |
| girişte otomatik bağlanan satır | `panel sign-in`, `path = "identity"`, `linked` toplamı | aynı satır |
| hesap sayfasından bağlama | `panel identity link`, `outcome = "linked"` (`"none"` = sonuç yok) | `accountidentity.go` |
| geçiş | `panel switched business` | `adminswitch.go` |
| geçiş reddi | `panel switch refused` · `panel switch refused: synchronizer missing or wrong` · `panel switch refused: no live admin session` · `panel switch budget reached` | `adminswitch.go` |

`TestAdminLogin_TheSignInLineNamesItsArm` iki `path` değerini ve satırda adres olmadığını sabitler.
SigNoz'da elle sorgu (ClickHouse SQL; **ölçülmedi** — kural dosyasının biçimiyle yazıldı, ilk koşu
orkestratörün salt-okuma sorgusudur):

```sql
SELECT toStartOfDay(fromUnixTimestamp64Nano(timestamp)) AS day,
       countIf(JSONExtractString(body,'msg') = 'panel sign-in' AND JSONExtractString(body,'path') = 'identity') AS identity_sign_ins,
       countIf(JSONExtractString(body,'msg') = 'panel sign-in' AND JSONExtractString(body,'path') = 'legacy') AS legacy_sign_ins,
       sumIf(JSONExtractInt(body,'linked'), JSONExtractString(body,'msg') = 'panel sign-in' AND JSONExtractString(body,'path') = 'identity') AS linked_at_sign_in,
       countIf(JSONExtractString(body,'msg') = 'panel identity link' AND JSONExtractString(body,'outcome') = 'linked') AS linked_on_account_page,
       -- M14-22: the link by e-mail's press (the account page's two-password form is gone).
       countIf(JSONExtractString(body,'msg') = 'panel link by e-mail' AND JSONExtractString(body,'outcome') = 'linked') AS linked_by_email,
       countIf(JSONExtractString(body,'msg') = 'panel switched business') AS switches,
       countIf(JSONExtractString(body,'msg') IN ('panel switch refused', 'panel switch refused: synchronizer missing or wrong',
                                                 'panel switch refused: no live admin session', 'panel switch budget reached')) AS switch_refusals
  FROM signoz_logs.distributed_logs_v2
 WHERE timestamp >= toUnixTimestamp64Nano(now64(9) - INTERVAL 7 DAY)
   AND resources_string['k8s.namespace.name'] = 'tappa' AND resources_string['k8s.container.name'] = 'tappa'
 GROUP BY day ORDER BY day
```

⛔ **Kullanıcı/operatör gerektiren:** bu sorguyu SigNoz'da bir pano olarak kaydetmek kümeye ve SigNoz
hesabına erişim ister (bu kartın yapıcısında yok); kural değil sayaçtır — `signoz-alert-rules.json`'a
**eklenmez** (o dosya uyarı kurallarıdır ve runbook tablosuyla satır satır eşlenir). Log saklama (Q28 (b),
15 gün) haftalık okumaya yeter, 60 günlük soak'un tamamına yetmez: her hafta okunan sayı `state.md`'ye
yazılır.

**Haftalık sayımlar** (salt okuma, yalnız sayı; sonuç `state.md`'ye — K-3 ve contract A'nın ön koşulu):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN TRANSACTION READ ONLY;
-- 1. Bağlanmamış satırlar (kimliksiz, reddedilmemiş): aktif | hepsi | e-postasız (hiçbir yoldan bağlanamaz).
SELECT count(*) FILTER (WHERE status = 'active')                                   AS unlinked_active_rows,
       count(*)                                                                    AS unlinked_rows,
       count(*) FILTER (WHERE email IS NULL OR btrim(email::text) = '')            AS unlinked_without_email
  FROM admin_users
 WHERE identity_id IS NULL AND unlinked_at IS NULL;
-- 2. Contract A'nın ön koşulu: aktif owner'ı olup hiçbir aktif owner'ı bağlı (ve reddedilmemiş) olmayan
--    işletme | bunların içinde tek aktif owner'lı olanlar (planın lafzı: "tek owner'ı bağlanmamış").
SELECT count(*)                         AS tenants_no_linked_active_owner,
       count(*) FILTER (WHERE owners = 1) AS tenants_single_owner_unlinked
  FROM (SELECT tenant_id, count(*) AS owners
          FROM admin_users
         WHERE role = 'owner' AND status = 'active'
         GROUP BY tenant_id
        HAVING count(*) FILTER (WHERE identity_id IS NOT NULL AND unlinked_at IS NULL) = 0) AS t;
-- 3. Ayrı: reddedilmiş satırlar ve işletmeleri (kullanıcı karar verir — ADR 0031 §5.12).
SELECT count(*) AS refused_rows, count(DISTINCT tenant_id) AS tenants_with_refused_rows
  FROM admin_users
 WHERE unlinked_at IS NOT NULL;
ROLLBACK;
SQL
```

beklenen: `BEGIN` · `A|U|X` · `T|S` · `R|RT` · `ROLLBACK`. Contract A ancak `T = 0` iken konuşulur
(ADR 0031 §8, plan M14-20a); `RT` ayrı raporlanır. Satır, adres, ad ya da kimlik id'si basılmaz
(CLAUDE.md §7). `internal/db/adminflag_test.go` bu bölümün sorgularını yazıldıkları gibi bir dünyada koşar.

### M14-22 — e-postayla bağlama: `00084`

Bayrak açıldıktan sonra kişinin aynı adresteki **bağlanmamış** satırları (M14-06'nın backfill'i farklı
parolalı satırları birleştirmedi) "Switch business"te görünmüyordu ve iki parolalı "Link a business"
kullanıcı kararıyla kalktı. Bu deploy'dan sonra bağlı bir oturumun adresinde bağsız aktif bir satır varsa
panelin her bölümünde bir bant çıkar (işletme adlandırmaz); "Add it" kimliğin adresine **tek** link gönderir,
link aynı tarayıcıda açılıp basılınca adresin **bütün** aktif, reddedilmemiş, bağsız satırları tek
transaction'da bağlanır (ADR 0031 §5.2 ve §5.7'nin M14-22 notları).

**`00084` ne yapar** (tek dosya, tek transaction; şema-yalnız, veri yazmaz): `admin_identity_verifications`'a
`purpose` sütunu (`'verify'` varsayılan; sıcak değil); `admin_users_linked_via_known` CHECK'ine `'email'`
(aynı ad; `admin_users` **sıcak** — `ACCESS EXCLUSIVE` küçük tablonun bir taraması kadar, `lock_timeout =
'1s'`; aşılırsa 55P03 → hiçbir şey yazılmaz, Job `Failed`, sakin bir anda yeniden); `identity_request_verification`
üçüncü argümanla (`p_purpose DEFAULT 'verify'`; eski binary'nin iki argümanlı çağrısı aynı davranışla çözülür);
yeni `identity_link_by_email`. **`identity_link_legacy`'ye dokunmaz** — önceki binary (fd53d34e) onu hesap
formundan ve girişin otomatik bağından çağırır, bu binary girişten çağırmaya devam eder; `REVOKE` kullanıcının
kararına bırakıldı (plan, M14-22'nin uygulama notu). Postcondition iki fonksiyonun biçimini, ACL'ini ve
`tappa_app`'ın `identity_link_legacy` `EXECUTE`'unun durduğunu doğrular.

**Rollout:** eski pod yeni şemada çalışır (iki argümanlı talep `DEFAULT`'la çözülür; eski form
`identity_link_legacy`'yi çağırmaya devam eder). Geri dönüş = önceki binary; **Down canlıda koşulmaz** (e-postayla
bağlanmış bir satır ya da canlı bir link varken reddeder).

**(a) Ön kontrol** (salt okuma, yalnız sayı; kullanıcı koşar):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN TRANSACTION READ ONLY;
SELECT max(version_id) AS schema_version FROM goose_db_version WHERE is_applied;
-- Adresinde bağsız, aktif, reddedilmemiş bir satır olan kimlik | o satırların sayısı | bunlardan yazdırılabilir
-- ASCII olmayan (homoglif — bağlanmaz, bant onlar için de çıkar: adlandırılmış kalıntı).
SELECT count(DISTINCT i.id)                                       AS identities_with_business_to_add,
       count(*)                                                   AS rows_to_add,
       count(*) FILTER (WHERE a.email::text !~ '^[\x21-\x7e]+$')  AS rows_not_ascii
  FROM admin_identities i
  JOIN admin_users a ON a.email = i.email
 WHERE a.identity_id IS NULL AND a.unlinked_at IS NULL AND a.status = 'active';
ROLLBACK;
SQL
```

beklenen: `BEGIN` · `83` · `1|1|0` (kullanıcının KM satırı; 2026-10-29'da tahmin — ölçülmedi) · `ROLLBACK`.
`rows_not_ascii > 0` → **DUR**, kullanıcıya bildir (bant o kişide link sonrası da kalır).

**(b) Deploy'dan sonra** (salt okuma; Job log'unda `OK   00084_link_businesses_by_email.sql`):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
BEGIN TRANSACTION READ ONLY;
SELECT max(version_id) AS schema_version FROM goose_db_version WHERE is_applied;
SELECT has_function_privilege('tappa_app', 'public.identity_link_by_email(text,text)', 'EXECUTE')                AS app_links_by_email,
       has_function_privilege('tappa_app', 'public.identity_request_verification(text,text,text)', 'EXECUTE')   AS app_requests,
       to_regprocedure('public.identity_request_verification(text,text)') IS NULL                              AS two_arg_form_gone,
       has_function_privilege('tappa_app', 'public.identity_link_legacy(text,text)', 'EXECUTE')                 AS app_keeps_link_legacy,
       has_function_privilege('tappa_operator', 'public.identity_link_by_email(text,text)', 'EXECUTE')          AS operator_links;
SELECT pg_get_constraintdef(oid) LIKE '%''email''%' AS linked_via_takes_email
  FROM pg_constraint WHERE conname = 'admin_users_linked_via_known';
ROLLBACK;
SQL
```

beklenen: `BEGIN` · `84` · `t|t|t|t|f` · `t` · `ROLLBACK`. Başka bir sonuç → migration'ın koştuğu sürüm yanlış;
incele.

**(c) Duman** (`https://taptime.mt/admin`, kullanıcının kendi hesabı: KF'de bağlı kimlik, aynı adreste KM'de
bağsız satır, kendi posta kutusu):

1. KF'ye kimlikle gir. Her bölümün tepesinde bant: *"Another business uses your email. Add it to this
   sign-in."* — KM'nin adı ya da bir sayı **yok**.
2. **Add it** → *"Check your inbox"* sayfası (işletme adı yok). Posta kutusunda *"Add your businesses to your
   Taptime sign-in"*.
3. Linki **aynı tarayıcıda** aç → *"Add your businesses"* sayfası, tek düğme **Add them** (açmak bir şey
   değiştirmez). Bas → *"Done. Every business that uses your email address is now on this sign-in."*; ikinci
   e-posta *"1 business was just added …"* (ad yok). Bant artık çıkmaz.
4. **Switch business** → KF ve KM listelenir; KM'ye **tek tıkla** geç; shell KM'yi adlandırır.
5. Geçişten önce açık kalmış bir KF sekmesinden bir form gönder → `409` *"This page belongs to another business —
   reload it."*
6. KM'nin **eski** parolasıyla giriş dene → genel hata (artık çalışmaz); kimlik parolası KM'ye de girer.
7. İkinci bir tarayıcıda gir; ilkinde Hesap → **Sign out everywhere** → iki tarayıcı da, **KM dahil** her
   işletmede çıkmış olur (artık bağlı olduğu için).
8. (a)'yı yeniden koş: beklenen `84` · `0|0|0`.

Herhangi bir adım 5xx verirse → geri dönüş önceki binary'dir (şema 84'te kalır; eski binary onu kullanır,
`'email'` bağları sıradan bağlı satırlardır).

### Panel formlarının tenant bağı (M14-10a)

Her panel POST formunun `action`'ı `?tb=<bağ>` taşır (ADR 0031 §5.5): oturumun işletmesinin, oturumun
**ham** token'ından türetilmiş anahtarla MAC'i (`internal/adminauth/formbind.go`). İki yazma zinciri
(`ProtectWriting`, `protectWritingThroughSuspension`) bağı gövdeye dokunmadan, askı halkasından ve her
yazmadan **önce** karşılaştırır; uyuşmazsa **409** *"This page belongs to another business — reload it."*
Bayraktan bağımsız, her zaman açık. Tek adlandırılmış istisna plaket encode rölesinin üç rotası (form
değil; Android rölesi sayfa çizmez ve bağı öğrenemez — kullanıcı kararı 2026-10-26, ADR 0031 §5.5'in
M14-10a notu 1; onları Origin, oturum, oturum bütçesi, askı ve encode tavanı korumaya devam eder).

**Adlandırılmış bedeller:**

- **Bu binary'nin rollout'u:** eski binary'nin çizdiği sayfalarda bağ yok. Rollout'tan sonra açık kalmış
  her panel formu **bir kez** 409 alır ve hiçbir şey yazmaz; sayfayı yenilemek çözer. Destek cümlesi:
  *"sayfayı yenileyin"*.
- **`TAPPA_SESSION_HMAC_KEY` rotasyonu:** aynı anahtar hem `admin_sessions.token_hash`'i
  (`internal/adminauth/token.go`, `deriveKey`) hem bağı anahtarlar. Rotasyonda **önce oturumlar ölür**:
  açık bir formun POST'u çözümleyicide düşer ve `303 → /admin/login` alır (409 değil), hiçbir şey
  yazılmaz; herkes yeniden girer. İleride oturumları rotasyondan geçiren bir ikili anahtar eklenirse açık
  formların payı 409 olur. İki durumda da bedel: açık her panel formu bir kez reddedilir.

**Bağ hiçbir URL'de kalmamalı (S14) — ölçülenler:**

- Uygulama log'u yalnız rota desenini yazar, URL'yi değil (`internal/httpx/requestlog.go`, `AccessLog`).
- Panel `<meta name="referrer" content="no-referrer">` gönderir (`web/templates/layout/base.templ`).
- Kapı bağı isteğin URL'sinden **siler**; handler onu göremez, yönlendirme hedefi taşıyamaz. Her bağlı
  rotada `Location`'da `tb` yok (`TestFormBinding_NoRedirectCarriesTheBinding`).
- **Sayfayla cevap veren POST'lar** (doğrulama hatasında yeniden çizilen form, inceleme/onay adımları,
  bir kez gösterilen anahtar/link ekranı) adres çubuğunda ve geçmişte `?tb=…`'yi bırakır. **Kabul
  edildi** (kullanıcı kararı 2026-10-26, ADR 0031 §5.5 not 2): bağ o oturumun ham çerezi olmadan ne
  üretilebilir ne de tek başına oynatılabilir. PRG yönlendirmeyle biten her yazmada geçerli (yukarıdaki test).
- **Ingress erişim log'u panel yollarında KAPALI** (kullanıcı kararı 2026-10-26, ADR 0031 §5.5 not 3).
  Ölçüm (yalnız repodan, 2026-10-23): `ingress-nginx` ConfigMap'i boş (`deploy/k8s/40-ingress.yaml`'ın
  ölçüm notu), yani denetleyicinin erişim log'u açık ve varsayılan `log-format-upstream` `"$request"`'i
  — sorgu dahil tam istek satırını — yazar; ADR 0020 de *"Ingress erişim log'u URL'yi yazar"* der.
  Değişiklik: `40-ingress.yaml`'a üçüncü Ingress **`tappa-panel`** — host `taptime.mt`, tek yol `/admin`
  (Prefix; panel çerezinin `Path`'i), TLS/sınıf/backend/etiketler ve notlar `tappa`'nınkiyle aynı,
  artı `nginx.ingress.kubernetes.io/enable-access-log: "false"`; cert-manager issuer notu yalnız
  `tappa`'da (tek sır `taptime-tls`, tek Certificate — 41'in konsol/legal çiftinin kuralı). `tappa`
  `/`'yi yönlendirmeye devam eder; ingress-nginx en uzun öneki seçtiği için `/admin` ve altı **yalnız**
  `tappa-panel`'e gider. Bedel: panel istekleri ingress erişim log'unda görünmez (süreç log'u rota
  desenini yazmaya devam eder). Pin: `TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog`.
  **Kabul edilen bedel:** not yalnız erişim log'unu kapatır; denetleyicinin **hata** log'u, nginx'in
  kendisinin düşürdüğü bir panel isteğinde (1m'yi aşan logo gövdesinin 413'ü, upstream bağlantı hatası,
  zaman aşımı) istek satırını sorgusuyla yazmaya devam eder — gerekçe kararın 2. maddesiyle aynı (bağ ham
  çerez olmadan işe yaramaz; ADR 0031 §5.5 not 3). Yapılandırma değişmez.
  Deploy aynı `kubectl apply -f deploy/k8s/40-ingress.yaml` adımıyla uygular (RBAC'ta `create` var).

  **Doğrulama (deploy'dan sonra, küme yöneticisi, salt-okur).** Önce iki nesne, yolları ve log notu;
  beklenen `tappa` için `/` ve `<none>`, `tappa-panel` için `/admin` ve `false`:

  ```bash
  kubectl -n tappa get ingress tappa tappa-panel \
    -o custom-columns='NAME:.metadata.name,PATHS:.spec.rules[*].http.paths[*].path,ACCESSLOG:.metadata.annotations.nginx\.ingress\.kubernetes\.io/enable-access-log'
  ```

  Sonra panelde bir sayfa açıp bir form gönderdikten sonra, denetleyicinin log'unda panel satırı
  sayısı — yalnız sayı basar; denetleyici bir DaemonSet ise `deploy/` yerine `ds/`. Beklenen `0` ve `0`
  (öteki yollar, ör. ` /t?`, log'da görünmeye devam eder). ⚠️ İki sayı hata satırlarını da sayar
  (`logs` erişim ve hata log'unu birlikte basar): `0` yalnız o pencerede panel isteklerinde nginx hatası
  olmadıysa beklenir; `> 0` görülürse önce o satırların hata satırı olup olmadığına bakılır:

  ```bash
  kubectl -n ingress-nginx logs deploy/ingress-nginx-controller --since=10m | grep -c ' /admin'
  kubectl -n ingress-nginx logs deploy/ingress-nginx-controller --since=10m | grep -c 'tb='
  ```

---

## Webhook (M11 API-9) — `TAPPA_WEBHOOK_KEK` ve düğüm/ingress adres listesi

API-9'dan beri `TAPPA_ENV=dev` **dışında** iki değer **zorunludur**; biri yoksa `config.Load` açılışı
reddeder (ADR 0028 §6, §7):

| Değer | Nerede | Ne |
|---|---|---|
| `TAPPA_WEBHOOK_KEK` | `tappa-secrets` (sır) | uç noktaların imza anahtarlarını `sun.Seal` ile mühürler; 32 bayt base64, **her öteki anahtardan farklı** (`openssl rand -base64 32`, ayrı üretilir). `20-app.yaml` onu **isteğe bağlı olmayan** `secretKeyRef` ile alır: Secret'ta yoksa kubelet pod'u başlatmaz. |
| `TAPPA_WEBHOOK_BLOCKED_ADDRESSES` | `05-config.yaml` (sır değil) | bir webhook'un **asla** bağlanamayacağı düğümün kendi genel adresleri ve ingress'in adresi; virgülle IP ya da CIDR. Özel, loopback, link-local ve ayrılmış adresleri `internal/egress` IANA kaydından zaten reddeder — bu liste kuralın bilemediğidir. Bugünkü değer tablonun altında. |

Listenin bugünkü iki girdisi: `144.76.158.60` — apex kaydının ölçülen origin'i (`40-ingress.yaml`) — ve `2a01:4f8:200:3164::/64` — düğümün IPv6 ağı (2026-10-16'da düğümde `ip -6 addr show scope global` → `2a01:4f8:200:3164::2`, `eno1`; Hetzner bu /64'ü yalnız bu sunucuya yönlendirdiği için tek adres değil **bütün /64**). `TestEgress_TheShippedListRefusesTheNodesIPv6Network` ConfigMap'in değerini `config.Load`'dan geçirir ve kapının ikisini de reddettiğini ölçer.

**Deploy'dan önce (API-13):** (1) `TAPPA_WEBHOOK_KEK` değeri `tappa-secrets`'a (ya da Infisical
`/tappa/`'ya — `externalsecret.example.yaml` onu artık zorunlu çeker, eksik uzak değer bütün senkronu
durdurur) **önce** yazılır; değer ekrana basılmaz, ayrımı öteki anahtarlarla **değer basmadan** ölçülür
(API-3'ün çift ölçümü) — ✅ **yapıldı (2026-10-16)**: Infisical `shared-infra/prod:/tappa` ve canlı
`tappa-secrets`, değer basılmadan. (2) Düğümün IPv6 adresi — ✅ **yapıldı (2026-10-16, M11 API-13a)**:
ölçülen `2a01:4f8:200:3164::2`'nin /64'ü listede. Kümeye düğüm ya da yük dengeleyici eklenirse adresi listeye
**aynı değişiklikte** eklenir. (3) Ölçüm (deploy sonrası, "M11 — deploy günü" (e)):
panelden kendi `https://taptime.mt/` adresine bir uç nokta kurulup ping atılır → sonuç
`blocked_address` olmalıdır.

---

## M11 — deploy günü (API-13): sıra, ölçümler, geri dönüş

**Ne:** `m11-api` dalının `main`'e birleşmesi = entegrasyon API'sinin, holding'in ve webhook'un canlıya
çıkışı (plan `docs/plan/m11-entegrasyon-api.md` §5 API-13). API-13a (2026-10-16) deploy'u beklemeyen her
kod ve manifest işini yaptı; aşağıdaki sıra **deploy günü kalanı** tek yerde toplar. Bir adım
geçmeden sonrakine geçilmez. `main`'e birleştirme bir **deploy kararıdır** (CLAUDE.md §10) — kullanıcının.

### Deploy'dan önce

**(a) OP-15'siz çıkış — 🟢 kullanıcı kararı (2026-10-17: *"M11 şimdi çıksın"*).** M11, OP-15'i (askı)
beklemeden canlıya çıkar; API-13a'nın *"OP-15 önce canlıda, `00057` onun üstüne yeniden kurulur"* yönü
**tersine döndü**: önce M11 (`00037`…`00057`), OP-15 **sonra**, `00058` olarak `00057`'nin üstüne.

- **OP-15'siz `/v1` yazmaları açıktır.** Askı kontrol noktası (ADR 0028 §10) `api.NoSuspensionColumn` ile
  kurulur ve her işletmeyi *askıda değil* sayar. Prod'da askı kavramı **yoktur** (`tenants.suspended_at`
  yok, operatör ekranında askı yok) — yani bu cevap prod'un gerçeğiyle tutarlıdır; hiçbir askı yok
  sayılmıyor. Panel ve dokunuş yolu da bugün askıyı bilmez.
  → **2026-10-20:** OP-15 (A + B) ve API-4b dal `op-15`'te birleşti: askı sütunu `00063`'tedir (`00058` değil —
  arada M11'in sonraki migration'ları geldi); `/v1`'in kontrol noktası `tenant.WriteSuspension` ile sütunu
  yazmanın kendi işleminde okur (`NoSuspensionColumn` kaldırıldı), panelin yazmaları `ProtectWriting`'in son
  halkası `suspensionGate`'te kapanır (ADR 0025 *"B fazı eki"*), dokunuş yolu askıyı yine bilmez (§4.6).
- **Kötüye kullanımda** (bir işletmenin anahtarı sızdı, entegratör hatalı yazıyor): operatör ekranında
  işletmenin özeti → *Integration API* → **"Close the API"** — anahtarlar ve webhook bir sonraki istekten
  itibaren 403 (K-22; (f)'nin ilk maddesi). Askının yerine geçen araç budur.
- **OP-15 sonra gelirken** (başka oturumun işi; bekleyen dallar `hold/op15*`):
  1. Migration dosyası `00036`'dan (dallarda `00035`) **`00058`**'e taşınır. `00036` boş kalır: goose
     uygulanmış en yüksek sürümün (`57`) altındaki yeni bir dosyayı sıra dışı (missing) diye **reddeder** —
     depo allow-missing kullanmaz (`Makefile`, `30-migrate-job.yaml` düz `up`).
  2. Ön koşulu ve değiştirdiği her nesne **M11'in son hâline göre** yazılır, 00034'ünkine göre değil:
     `op_read_tenant_detail` `00057`'nin gövdesi + iki askı sütunu (`suspended_at`, `suspension_reason`)
     satırın **sonunda** → **13 sütun** (`00057`'nin 11'i: 00029'un 9'u + `api_enabled_at`, `api_code`;
     ardından askının 2'si); ön koşulun sonuç tipi metni ve `md5(prosrc)`'i `00057`'ninkiler. Aynı kural
     OP-15'in değiştirdiği öteki nesnelere de uygulanır — `op_begin_read`, `op_read_audit`,
     `operator_audit_log_kind_check` ve `operator_audit_log_tenant_write_shape` en son `00042`'de
     değişti (`00039` ve `00042`'nin tür adları korunur), `op_read_tenants`'a M11 dokunmadı. Down
     `00057`'nin (ve `00042`'nin) hâline döner, 00029'a / 00034'e değil. `tenants`'a `ALTER` aldığı için
     ilk ifadesi `SET LOCAL lock_timeout = '1s'` (aşağıdaki kural, `00037`/`00038` emsali).
  3. Go: `internal/db/operator.go` `readTenantDetailSQL` ve `readTenantDetail`'in `Scan`'i 13 sütuna.
  4. **API-4b zorunlu olur, aynı birleştirmede:** `cmd/tappa/apiwiring_test.go`
     `TestAPIWiring_TheSuspensionReaderFollowsTheColumn` migration'larda `suspended_at`'i görünce CI'ı
     kırmızıya çevirir → `NoSuspensionColumn` yerine sütunun okuması + §4.7'nin hücre hücre testleri
     (plan §5 API-4b). Deploy CI yeşil olmadan koşmaz (`workflow_run`) — yani OP-15 API-4b'siz canlıya
     **çıkamaz**; kapı kapalı düşer.
     → **2026-10-20:** yapıldı — API-4b `op-15`'e birleşti, `TestAPIWiring_TheSuspensionReaderFollowsTheColumn`
     yeşil; migration numarası `00063` oldu (1. maddedeki `00058` o günün tahminidir).

  Yanlış sıra yüksek sesle düşer: `00036` numarasıyla gelen bir OP-15'i goose durdurur (yukarıda 1.);
  `00058`'in ön koşulu `00057`'nin sonuç tipini ve `md5(prosrc)`'ini sınar (00039'un ve `00057`'nin
  kalıbı), böylece M11'in iki API sütununu sessizce geri alacak bir gövde 55000 ile reddedilir.
  `00057`'nin kendi ön koşulu (00029'un gövdesini ister) prod'da bir kez, OP-15'ten **önce** koşar; onu
  tetikleyen tek durum `00057`'den önce `op_read_tenant_detail`'i değiştiren bir migration'dır, ki bu
  kararla artık olmayacak. **Doğru sıra: M11 (`00037`…`00057`) → OP-15 (`00058`).**

**(b) Küme rolleri ve sırlar.**
- Rol blokları **canlı veritabanında** bir kez koşulmuş olmalı: *"Holding role (M11 API-1a)"* ve
  *"Feed role (M11 API-2)"* bölümleri (`00041` ve `00048` rolleri yaratmaz, yoksa migration Job'ı
  durur ve rollout yapılmaz). ⏳ Ana oturum deploy'dan **önce** koşar (2026-10-17 itibarıyla bekliyor).
- `TAPPA_API_KEY_HMAC_KEY` ve `TAPPA_WEBHOOK_KEK` — ✅ **yapıldı (2026-10-16, ana oturum)**: Infisical
  `shared-infra/prod:/tappa` ve canlı `tappa-secrets`, ikisi de 32 bayt, sekiz 32 baytlık anahtar çift
  çift farklı (değer basılmadan ölçüldü). Yeniden ölçüm **yalnız ad ve boyut** basar:

  ```bash
  kubectl -n tappa describe secret tappa-secrets | grep -E '^(TAPPA_API_KEY_HMAC_KEY|TAPPA_WEBHOOK_KEK):'
  ```

  beklenen: her biri "44 bytes" (32 baytın base64 metni) — değer basılmaz

- `TAPPA_WEBHOOK_BLOCKED_ADDRESSES` — ✅ **yapıldı (API-13a)**: IPv4 origin + düğümün IPv6 /64'ü
  (*"Webhook (M11 API-9)"* bölümü).
- Prod salt-okur sayımlar (ana oturum, 2026-10-16): goose **35**; tenant sayısı **tek haneli** (T124
  eşiğinin çok altında); `audit_log` **birkaç yüz** satır (`00054`'ün kısmi indeksi anında kurulur);
  isim kuralına takılan ad (lokasyon/departman/çalışan) **0**. Kesin sayılar depoya yazılmaz.
- `TAPPA_INVITE_DELIVERY` ConfigMap'te `email` (`TestPackaging_TheConfigMapShipsTodaysDelivery`):
  API'nin davetleri yalnız `email` kipinde gönderilir.
- **`tappa-security-auditor` tam turu** `m11-api`'nin `main`'e göre farkı üstünde (plan API-13) — ✅ **ONAY**
  (2026-10-17); üç DÜŞÜK'ü API-13b kapattı (`00037`/`00038`'in `lock_timeout`'u, `20-app.yaml`'daki
  `TAPPA_API_HOST` Secret girdisi, bu bölümün (a)'sı).

**(c) DNS** — ✅ **yapıldı (2026-10-17, ana oturum):** `api` A → origin, DNS-only, AAAA yok. Kural (yeniden
kurulursa): `taptime.mt` bölgesine
**`api` A kaydı → `144.76.158.60`, proxy KAPALI (gri bulut, DNS-only)**, birleşmeden **önce** (HTTP-01
sertifikası adın çözülmesini ister; deploy'un Cloudflare ön kontrolü ve `api-host` kapısı ad çözülmezse
uyarıp geçer, çözülürse ölçer). **AAAA kaydı açılmaz**: apex'in de yok, ve IPv6 yolunda ingress'in
dinlediği ve `TAPPA_TRUSTED_PROXIES`'in gerçek istemci adresini verdiği ölçülmedi — açılması ayrı bir
ölçüm ister. Kayıttan sonra: `dig +short api.taptime.mt` → `144.76.158.60` (bir Cloudflare kenar adresi —
`104.21.*` / `172.67.*` — proxy açık demektir).

### Deploy

**(d) Birleştirme → CI → deploy.** `.github/workflows/deploy.yml`'in birleşik sırası (`main`'in T119 düzeni +
M11; API-13b, 2026-10-17): imaj adımları → Cloudflare ön kontrolleri **`taptime.mt` → `ops.taptime.mt` →
`api.taptime.mt`** (üçü de ilk `kubectl apply`'dan önce) → Secret ön kontrolü (`tappa-secrets` var mı) →
`opsurface` ön kontrolü (`verify-deployment.sh operator-surface` → `on`/`off`) → ConfigMap (`TAPPA_API_HOST`
dahil), init/yedek ConfigMap'leri, Postgres → migration Job (**37…57** — `00036` yok ve prod'da da olmayacak —
OP-15 (a) gereği sonra `00058` olarak gelir; `00037`/`00038` `employees`/`locations`/`tenants`'ta
`ACCESS EXCLUSIVE`, kendi `lock_timeout`'larıyla — 02:30 yedeğinden uzak bir saatte; `00054` kısmi indeks —
ölçülen boyutta anında; `00056` `employees` indeksi kendi `lock_timeout`'uyla; `00057` yalnız bir fonksiyon,
00029'un gövdesi + iki API sütunu) → *"Roll out the server"*: `20-app.yaml` → `40` → **rollout beklemesi** →
**`db-role` kapısı** (OPS-1, 2026-10-18: rollout'un hemen ardından, host Ingress'lerinden ve host
kapılarından **önce** — eskiden en sondaydı ve düşen bir host kapısı onu hiç koşturmuyordu; düzeltme turunda
41/42'nin `apply`'ı ve `case`'in `exit 1` dalı da rollout adımından çıktı, çünkü onlardan biri düşse yine
koşmuyordu; sunucunun migration rolüyle bağlı olmadığı, verinin RLS'i hakkındaki tek ölçüdür) →
*"Route the operator and API hosts"*: `41` (yalnız `opsurface` `on` ise; `case` içinde) →
`42-api-ingress.yaml` (`case`'in **dışında** — API host'u operatör yüzeyine bağlı değil; ikisi de hâlâ
rollout beklemesinden **sonra**) → *"Gate — the operator host must
not serve the customer product"* (`operator-host` kipi) → *"Gate — the API host serves the API and nothing
else"* (`api-host` kipi: `/v1/guide.md` 200 `text/markdown`, kök 404). Migration Job `Failed` olursa rollout
yapılmaz (*"4. Migration Job'ı Failed"*). Pinler: `TestPackaging_TheDeployAppliesEveryIngressFile` (40
rollout adımında beklemeden önce; 41 ve 42 yalnız *"Route the operator and API hosts"*ta, 42 41'in
`case`'inin dışında; üç Ingress dosyası) ve `TestPackaging_TheDeployGatesTheCustomerAndAPIHosts` (OPS-1'den
beri sıra: rollout → `db-role` (rollout+1) → host yönlendirme (rollout+2) → iki host kapısı).

**Birleştirme çakışması — `main`'in T119 değişiklikleri** — ✅ **birleştirildi (API-13b, 2026-10-17, merge
commit `Merge origin/main into m11-api`; `origin/main` = `1ce79a3`; `main`'den migration gelmedi).** Gerçek
çakışma altı dosyadaydı (`deploy.yml`, `verify-deployment.sh`, `40-ingress.yaml`, bu dosya, `docs/backlog.md`,
`docs/plan/state.md`); `operatoringress_test.go`, `querier.go` ve öteki belgeler kendiliğinden birleşti
(`make gen` `querier.go`'yu değiştirmedi). Birleştirmenin kendi düzeltmeleri: `main`'in `operatorsurface_test.go`'su
da bir `stubCurl` tanımladığı için API host kipinin sahte `curl`'ü yeniden adlandırıldı; `main`'in her-dal
testi (`TestVerifyDeployment_EveryBranchTouchesTheSecretOnlyByDescribe`) artık `api-host` kipinin beş
uyarısını da sürüyor; `all` kipi `main`'inki kaldı (`api-host`'u içermez — deploy onu ayrı koşar).
*(OPS-1, 2026-10-18: `all` artık `kubeconfig-server`'ı ve `api-host`'u da koşar; `tailnet`'i bilerek
koşmaz — yalnız `tag:ci`'li runner'da geçebilir, elle koşan operatörün cihazında her seferinde kırmızı
olurdu. Gerekçe `scripts/verify-deployment.sh` başlığında.)*
Aşağıdaki metin birleştirmeden önce yazılmış kurallardır. Bu dal `main`'den
`bcbff6d`'de ayrıldı; o noktadan bu yana `main` 25 dosya değiştirdi ve **11'i** bu dalın da değiştirdiği
dosyalardır (2026-10-16, `git diff origin/main...m11-api --stat` ve `git log origin/main` ile okundu;
birleştirme **denenmedi**, `origin/main` = `1ce79a3`): `.github/workflows/deploy.yml`,
`scripts/verify-deployment.sh`, `deploy/README.md`, `deploy/k8s/40-ingress.yaml`,
`cmd/tappa/operatoringress_test.go`, `internal/store/querier.go`, `docs/adr/0020-…`, `docs/backlog.md`,
`docs/plan/m10-platform.md`, `docs/plan/open-questions.md`, `docs/plan/state.md`. `main`'in T119 düzeni
(`80e25c1`): apex'in Cloudflare ön kontrolünün hemen ardına `ops.taptime.mt`'nin ön kontrolü (bu dalın
`api.taptime.mt` ön kontrolüyle **aynı yer**); Secret ön kontrolünden sonra `opsurface` ön kontrolü
(`verify-deployment.sh operator-surface` → `on`/`off`); *"Roll out the server"*da **41 rollout
beklemesinden sonra** ve yalnız `on` iken; ardından *"Gate — the operator host must not serve the
customer product"* (`operator-host` kipi). Yukarıdaki (d)'nin `40, 41 → rollout → 42` sırası **T119
öncesine** aittir; birleştirmede `main`'in düzenine göre yeniden yazılır: `20 → 40 → rollout beklemesi →
41 (opsurface on ise) → 42 → operatör host kapısı → api-host kapısı → db-role`. Çözüm kuralları:
- `deploy.yml`: iki ön kontrol (`ops`, `api`) ikisi de kalır, ikisi de ilk `kubectl apply`'dan önce;
  42'nin uygulanması 41'in `case`'inin **dışında** (API host'u operatör yüzeyine bağlı değil).
- `operatoringress_test.go`: `TestPackaging_TheDeployAppliesEveryIngressFile`'ın sıra iddiası
  `main`'inki (`40 < bekleme < 41`) **artı** bu dalın 42 iddiası, Ingress dosyası sayısı **3**;
  `TestPackaging_TheDeployGatesTheCustomerAndAPIHosts` değişmeden kalır (iki adımı o pinler).
- `verify-deployment.sh`: iki dalın kipleri yan yana (`operator-surface`, `operator-host`; `api-host`) ve
  kullanım metninde ikisi de; `TestVerifyDeployment_TheAPIHostModeJudgesEveryAnswer` yeşil kalır.
- `internal/store/querier.go` **elle çözülmez**: `db/queries/policies.sql` (`main`'in T121'i) birleşir,
  sonra `make gen`.
- `40-ingress.yaml`, `deploy/README.md`, belgeler: iki tarafın metni de kalır (T119 bölümleri ve bu
  bölüm). Ardından `make check` ve `go test ./cmd/...`.

### Deploy'dan sonra — ölçümler (plan API-13'ün listesi, tek yerde)

**(e)** Her biri bir komut ya da bir ekran; sonuç state.md'ye yazılır.

1. **Proxy kapalı, sertifika geçerli** (`-k` YOK):
   `curl -sS -o /dev/null -D - https://api.taptime.mt/v1/guide.md | grep -iE '^(HTTP/|cf-ray|server|content-type|etag)'`
   → `HTTP/2 200`, `text/markdown; charset=utf-8`, bir `ETag`, **`cf-ray` yok**.
2. **Rehber önbelleği ve host ayrımı:** aynı isteğe `-H 'If-None-Match: <ETag>'` → **304**;
   `https://api.taptime.mt/v1/guide.tr.md` ve `/v1/openapi.yaml` → 200 (anahtarsız);
   `https://taptime.mt/v1/guide.md` → **404**; `https://api.taptime.mt/` ve `/admin/login` → **404**.
3. **Çift noktalı host yönlendirilmez** (ölçülmedi, burada ölçülür):
   `curl -sk -o /dev/null -w '%{http_code}\n' -H 'Host: api.taptime.mt..' https://api.taptime.mt/v1/guide.md`
   → **400 ya da 404** — uygulamaya ulaşmaz (rehber değil, panel sayfası değil). Büyük olasılıkla
   **400**: nginx'in `ngx_http_validate_host`'u art arda iki noktayı geçersiz host sayar ve isteği
   sunucu seçiminden önce reddeder; kural host'u birebir yazdığı için geçerli sayılsa da adın sunucusu
   yoktur → controller'ın varsayılan sunucusu (404). Hangisi olduğu ölçülemedi (küme erişimi yok); 200
   ya da bir panel sayfası hatadır.
4. **Webhook engel listesi:** panelden `https://taptime.mt/` ve `https://api.taptime.mt/` adresli bir uç
   nokta kurulup ping atılır → ikisi de **`blocked_address`**.
5. **K-22 — operatör ekranından** (`ops.taptime.mt` → *Tenants* → işletmenin özeti → *Integration API*):
   KF'nin özetinde kod **`KF`** ile *"Open the API"* → *"Opened."*, durum *Open* + açılma anı; KM'de
   **`KM`**. *Audit log*'da `tenant_api_enabled` satırları; işletmelerin kendi iz kayıtlarında
   `tenant.api_enabled`. Kapatma aynı bölümdeki *"Close the API"* (kod saklanır).
6. **KF+KM holding'i:** *Holdings* → bir holding oluştur → KF'yi ve KM'yi ekle (ikisi de `Europe/Malta`)
   → ikisi de *"Added."* ve canlı üye. Kod kuralı: bir üyenin kodu holding'deki başka üyede olamaz.
7. **K-18 bağlama (kullanıcı, panelde):** KF ve KM panellerinde her lokasyonun **External ref**'i
   KF-RMS / KM-ERP'deki kimliğe yazılır; çalışanlar ilk bağlamada **aynı işletmede e-posta
   eşleşmesiyle** bağlanır (K-18; plan §7 madde 4).
8. **Davet yolu:** `kubectl -n tappa get cm tappa-config -o jsonpath='{.data.TAPPA_INVITE_DELIVERY}'` →
   `email`; sürecin açılış satırı davet kuyruğunun açık olduğunu söyler.
9. **Gerçek istemci adresi (oran sınırı):** dış bir ağdan bir API isteği → erişim kaydındaki istemci
   adresi çağıranın genel adresidir, `10.42.0.1` değil (`TAPPA_TRUSTED_PROXIES`, `05-config.yaml`).
10. **Holding dışı izolasyon örneği:** holding dışı bir işletmenin (C) anahtarıyla `GET /v1/punches?scope=employer`
    ve `GET /v1/directory` → KF/KM'den **hiçbir** satır yok; KF'nin anahtarıyla `GET /v1/directory` →
    KF + KM, C yok.
11. **Webhook teslimi:** K-22'si açık, uç noktası olan bir işletmede bir dokunuş → panelin *Deliveries*
    listesinde `Delivered` (ya da kapalı bir sınıf); log satırı yalnız kimlik + sınıf + durum taşır.
12. **Rol üyelikleri:** `SELECT r.rolname, m.rolname FROM pg_auth_members a JOIN pg_roles r ON r.oid = a.roleid
    JOIN pg_roles m ON m.oid = a.member WHERE r.rolname IN ('tappa_opdefiner','tappa_holdingdefiner','tappa_feedwriter','tappa_operator')
    OR m.rolname IN ('tappa_opdefiner','tappa_holdingdefiner','tappa_feedwriter','tappa_operator');` → **0 satır**.

### Geri dönüş

**(f)** 🔴 **Migration Down'ları prodda KOŞULMAZ** — düzeltme **ileri** bir migration'dır (CLAUDE.md §6:
uygulanmış migration değiştirilmez). 🔴 **`main`'i geri almak migration'ı geri ALMAZ**: geri alınan bir
commit'in deploy'u eski imajı **goose 57 şemasına** karşı başlatır; M11 öncesi bir ikilinin bu şemayla
çalıştığı **ölçülmedi** — geri dönüş yolu bu yüzden imajı geri almak değil, ileri bir düzeltmedir.
Deploy'suz hızlı kapatmalar:
- bir işletmenin API'si (anahtarlar + webhook): operatör ekranında *"Close the API"* — bir sonraki
  istekten itibaren 403;
- bir webhook uç noktası: işletmenin panelinde uç noktayı kapatmak;
- API host'unun tamamı: `TAPPA_API_HOST`'u `05-config.yaml`'dan **ve** `42-api-ingress.yaml`'ı **aynı
  commit'te** kaldırmak + Ingress nesnesini küme yöneticisinin silmesi (deploy kimliğinde `delete` yok —
  `01-rbac.yaml`): `kubectl -n tappa delete ingress tappa-api`. Önce Ingress silinir, sonra deploy —
  ters sıra adı müşteri ürününe yönlendirir (`42`'nin başlığı).

**(g) Baseline `2026-10-18` (T125, ADR 0029) — eski ikiliye dönmek yeni tenant'ları `review`'a düşürür.**
Bu sürüm `base:cross-location-note`'u `note` etkisiyle materyalize eder; bu sürümden önceki bir ikilinin
doğrulayıcısı `note`'u tanımaz. Geri dönülürse, arada materyalize olmuş her tenant'ın (yeni kayıtlar ve
ilk dokunuşunu bu sürümde yapanlar) saklı baseline'ı eski ikilide eksik sayılır ve dokunuşları **yalnız
guardrail'lerle** değerlendirilir: her dokunuş varsayılan `review`'a düşer, **kayıt yazılır** (fail-safe,
§4.6), `policy.guardrails_alone` (`reason=incomplete_after_materialise`) kural 8 ile çağırır (ölçüldü: `433e172`'nin
`forTenant`'ı bu sürümün materyalize ettiği bir tenant'ta); o tenant'ların sahipleri
panelde kural değiştiremez. 2026-07-26 baseline'ı saklı tenant'lar etkilenmez. Yapılacak: **ileri dön**
(bu sürüm ya da üstü); saklı belgeye dokunulmaz (`policy_versions` append-only); arada `review`'a düşen
kayıtları müdürler onay kuyruğundan onaylar. Etkilenen tenant sayısı (salt-okur):
`SELECT count(DISTINCT v.tenant_id) FROM policy_versions v JOIN policies p ON p.tenant_id = v.tenant_id AND p.id = v.policy_id WHERE p.layer = 'baseline' AND v.document @> '{"statements":[{"effect":"note"}]}'`.

---

## Sandbox (API-11) — kullanıcı adımları

**Ne:** entegratörlerin denediği ayrı Taptime (ADR 0028 §11, K-3): **ayrı alan adı** (`taptime.mt`'nin
alt alanı DEĞİL), **ayrı derleme** (`make build-sandbox`, `-tags sandbox`; Dockerfile hedefi
`app-sandbox`), **ayrı namespace** `tappa-sandbox` ve kendi Postgres'i, kendi sırları, kendi operatörü.
Kod kısmı API-11'de yazıldı ve CI'da koşar (`make check-sandbox`); bu bölüm **alan adı gerektiren**
kalanın tarifidir. Manifestler `deploy/sandbox/` altındadır ve **henüz uygulanmadı**; `deploy.yml`
sandbox imajını **derlemez, itmez, uygulamaz** (`TestSandboxManifests_AreTheSandboxsOwn` bunu tutar) —
sandbox'ın deploy yolu alan adıyla birlikte ayrı bir iştir.

**Ne değişmez:** sandbox da gerçek bir ortamdır, CLAUDE.md §4'ün hepsi geçerlidir. Simüle dokunuş
(`POST /v1/sandbox/taps`) plaketten sonrasını gerçek yoldan koşar (SUN ön kontrolü, atomik `ctr`,
karar motoru, `transactions` INSERT'i, tetikleyiciler); sanal plaketlerin anahtarları sandbox'ın KEK'iyle
sarmalanır; gerçek bir NXP UID'si (`04…`) her çözümlemede ve her eklemede reddedilir (yalnız
`0053…` sanal aralığı). **Prod yedeği sandbox'a asla geri yüklenmez**, prod sırları oraya asla
kopyalanmaz.

### 1. Alan adı

Kullanıcı bir alan adı alır — `taptime.mt`'nin alt alanı **olmayan**, kendi kaydı olan bir ad (aşağıda
`$D`). Üç host kullanılır: `$D` (panel, `/signup`, dokunuş sayfası), `api.$D` (yalnız `/v1`) ve
`ops.$D` (sandbox'ın kendi operatör yüzeyi). Alt alan olmadığı için `taptime.mt`'nin hiçbir çerezi
kapsamda değildir; çerez geçişi gerekmez (ADR 0028 §11).

Bu bölümün blokları alan adını ve imajın commit'ini **kabuk değişkenlerinden** okur; aynı kabukta bir
kez, ilk bloktan önce tanımlanır (tırnak içindeki iki değer kullanıcınındır: alan adı ve imajın
derlendiği commit'in 40 haneli SHA'sı; `TAG` ondan türetilir):

```bash
D='<sandbox-alan-adi>'
SHA='<40-haneli-commit-sha>'
TAG="sha-${SHA:0:12}"
```

### 2. DNS — DNS-only

Cloudflare'de (ya da alan adının sağlayıcısında) üç **A** kaydı, düğümün adresine
(`144.76.158.60`), **DNS-only** (turuncu bulut kapalı — cert-manager'ın HTTP-01'i ve
`TAPPA_TRUSTED_PROXIES`'in ölçümü proxy'siz kurulmuştur, `api.taptime.mt` emsali), **AAAA yok**.
Kontrol (kaydın yayıldığını, adresin düğüm olduğunu ölçer):

```bash
for h in "$D" "api.$D" "ops.$D"; do printf '%s ' "$h"; dig +short A "$h"; done
```

Üç satırın üçü de `144.76.158.60` olmalı.

### 3. Sırlar — sandbox'ın KENDİ anahtarları

Infisical'da **ayrı bir yol** (öneri: `shared-infra/prod:/tappa-sandbox`; `/tappa` ile **asla** ortak
değil). `deploy/sandbox/secret.example.yaml`'ın her anahtarı **yeniden üretilir**, prod'dan kopyalanmaz:
yedi 32 baytlık anahtar (`TAPPA_SESSION_HMAC_KEY`, `TAPPA_TAG_KEK`, `TAPPA_INVITE_HMAC_KEY`,
`TAPPA_API_KEY_HMAC_KEY`, `TAPPA_WEBHOOK_KEK`, `TAPPA_OPERATOR_TOTP_KEK`,
`TAPPA_OPERATOR_TOKEN_HMAC_KEY`) her biri ayrı bir `openssl rand -base64 32` ile; üç parola
(`POSTGRES_PASSWORD`, `TAPPA_APP_PASSWORD`, `tappa_operator`'ın) ve onlardan kurulan üç DSN;
`TAPPA_OPERATOR_HOST` = `ops.$D`. SMTP anahtarı **yoktur** (sandbox e-posta göndermez;
`config.Load` `TAPPA_ENV=sandbox` ile `TAPPA_RESET_DELIVERY=none` ve `TAPPA_INVITE_DELIVERY=panel`
dışını reddeder).

Neden ayrı: sandbox KEK'i prod'unkine eşit olsaydı sandbox'ta bir sanal plaketi açan anahtar bir prod
plaketini de açardı; anahtar-özeti anahtarı eşit olsaydı bir HMAC iki ortamda aynı cevabı verirdi.
Sırlar canlı Secret'a yazıldıktan sonra **prod'la hiçbirinin eşit olmadığı** değerler basılmadan
ölçülür — çıktı yalnız ad ve `differ`/`SAME` yazar:

```bash
for k in TAPPA_SESSION_HMAC_KEY TAPPA_TAG_KEK TAPPA_INVITE_HMAC_KEY TAPPA_API_KEY_HMAC_KEY TAPPA_WEBHOOK_KEK TAPPA_OPERATOR_TOTP_KEK TAPPA_OPERATOR_TOKEN_HMAC_KEY; do
  a=$(kubectl --context hetzner-k8s-1 -n tappa get secret tappa-secrets -o "jsonpath={.data.$k}" | shasum -a 256)
  b=$(kubectl --context hetzner-k8s-1 -n tappa-sandbox get secret tappa-secrets -o "jsonpath={.data.$k}" | shasum -a 256)
  if [ "$a" = "$b" ]; then echo "$k SAME"; else echo "$k differ"; fi
done
```

Tek bir `SAME` satırı varsa o anahtar sandbox'ta yeniden üretilir; sandbox başlamadan önce.

### 4. Uygulama — kim, nasıl

**Kimlik:** ilk kurulumu **küme yöneticisi** (kullanıcı) Tailscale üzerinden kendi kimliğiyle yapar
(`--context hetzner-k8s-1`; hafıza notu: varsayılan bağlam başka küme). Prod'un `github-deployer`'ı
**kullanılmaz** — onun yetkisi `tappa` namespace'ine göre kesilmiştir ve sandbox'ı ona açmak prod'un
deploy kimliğinin yüzeyini büyütürdü. Sandbox'ın kendi otomatik deploy'u gelince ona **ayrı** bir
kimlik (`tappa-sandbox` namespace'inde bir ServiceAccount + yalnız o namespace'e Role) açılır.

**İmaj:** aynı commit'ten, kendi hedefiyle; itmeden önce kapı derleme etiketini doğrular (prod imajı
`sandbox` etiketliyse, sandbox imajı etiketsizse reddedilir):

İmajlar prod'un private Harbor projesinde (OPS-2; operatör adımı 4). İten makine `robot$tappa-ci`'yle
(ya da bir proje yöneticisiyle) `docker login registry.everva.com.tr` yapmış olmalı.

```bash
docker build --target app-sandbox -t "registry.everva.com.tr/tappa/tappa-sandbox:$TAG" .
scripts/verify-image.sh --sandbox "registry.everva.com.tr/tappa/tappa-sandbox:$TAG" "$SHA"
docker push "registry.everva.com.tr/tappa/tappa-sandbox:$TAG"
```

Migration imajı prod'unkidir (migration'lar tek kümedir): aynı commit'in
`registry.everva.com.tr/tappa/tappa-migrate:$TAG`'i.

🔴 **`harbor-pull` bu namespace'te de olmalı** — pod bir `imagePullSecret`'ı yalnız **kendi**
namespace'inden okur, ve sandbox'ın iki pod spec'i de onu adlandırır. Operatör adımı 4 (c)'nin
bloğu, `-n tappa-sandbox` ile; Postgres'ten önce yapılabilir.

**Sıra** (her adım bir öncekinin bitmesini bekler). Önce namespace, kota, yapılandırma ve Postgres'in
ilk açılış betikleri:

```bash
kubectl --context hetzner-k8s-1 apply -f deploy/sandbox/00-namespace.yaml -f deploy/sandbox/02-quota.yaml
sed "s/SANDBOX_DOMAIN/$D/g" deploy/sandbox/05-config.yaml | kubectl --context hetzner-k8s-1 apply -f -
kubectl --context hetzner-k8s-1 -n tappa-sandbox create configmap tappa-db-init --from-file=scripts/db-init/01-roles.sql --from-file=deploy/k8s/postgres-init/02-app-password.sh
```

**`tappa-secrets` (3. adım) bu noktadan önce — Postgres'ten önce — namespace'te olmalıdır:** Postgres'in
ilk açılışı parolalarını ondan okur. Infisical'ın ExternalSecret'ı ya da `kubectl create secret
generic`, değer kabuk geçmişine düşmeden. Sonra Postgres ve ağ kuralı:

```bash
kubectl --context hetzner-k8s-1 apply -f deploy/sandbox/10-postgres.yaml -f deploy/sandbox/12-networkpolicy.yaml
kubectl --context hetzner-k8s-1 -n tappa-sandbox rollout status statefulset/tappa-postgres --timeout=5m
```

Ardından şema, sunucu ve host'lar:

```bash
sed "s/tappa-migrate:deploy-placeholder/tappa-migrate:$TAG/" deploy/sandbox/30-migrate-job.yaml | kubectl --context hetzner-k8s-1 apply -f -
kubectl --context hetzner-k8s-1 -n tappa-sandbox wait --for=condition=complete job/tappa-migrate --timeout=10m
sed "s/tappa-sandbox:deploy-placeholder/tappa-sandbox:$TAG/" deploy/sandbox/20-app.yaml | kubectl --context hetzner-k8s-1 apply -f -
kubectl --context hetzner-k8s-1 -n tappa-sandbox rollout status deployment/tappa --timeout=5m
sed "s/SANDBOX_DOMAIN/$D/g" deploy/sandbox/40-ingress.yaml | kubectl --context hetzner-k8s-1 apply -f -
```

`ops.$D`'nin Ingress'i bu dosyada **yoktur**: prod'un operatör host'u gibi bir adres izin listesiyle
(`deploy/k8s/41-operator-ingress.yaml`) yazılır; o yazılana kadar sandbox operatörü yüzeye
`kubectl port-forward` ile ulaşır.

### 5. Sandbox operatörü

Taze PGDATA rolleri `01-roles.sql`'den kendiliğinden alır (yukarıdaki *"Operator roles (M10 OP-5)"*,
*"Holding role (M11 API-1a)"* ve *"Feed role (M11 API-2)"* bölümlerinin elle adımları **gerekmez**).
Kalan, prod'daki adımların `-n tappa-sandbox` ile aynısıdır: `tappa_operator`'ın girişi ve parolası
(*"Operator surface (M10 OP-7)"*), ardından operatör hesabı `cmd/opadmin create … --host ops.$D`
(*"Operator accounts (M10 OP-9)"*). Sandbox operatörü her deneme işletmesinin API'sini **kendisi açar**
(*Open the API*, şirket kodu — K-22: açılmamış işletmenin `tt_test_` anahtarı 403 `api_disabled`
alır) ve holding denenecekse holding'i kurar.

### 6. Doğrulama

Üçüncü satırın anahtarı **açıkça sahtedir**: `tt_live_` öneki ve 43 `A` — bir prod anahtarının
biçimi, hiçbir anahtarın değeri değil. Sandbox ikilisi için bu, bozuk biçimli bir değerden farksızdır:
biçim kapısı yalnız kendi derlemesinin önekini (`tt_test_`) tanır, yanlış önek ve bozuk biçim **aynı
daldan** geçer (`apikey` biçim kontrolü → `ErrMalformed`) — cevap aynı `401`'dir ve veritabanına
gidilmez. Bu satır yanlış önekin bu daldan geçtiğini gösterir, ayrı bir ret türünü değil.

```bash
curl -sS -o /dev/null -w '%{http_code}\n' "https://api.$D/v1/sandbox/openapi.json"
curl -sS -o /dev/null -w '%{http_code}\n' https://api.taptime.mt/v1/sandbox/openapi.json
curl -sS -o /dev/null -w '%{http_code}\n' -X POST -H 'Authorization: Bearer tt_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' "https://api.$D/v1/sandbox/taps"
```

Beklenen: `200` (sandbox'ın belgesi), `404` (prod ikilisinde bu yol yoktur), `401` (yanlış önek,
bozuk biçimle aynı cevap — veritabanına gitmeden). Sunucunun açılış satırı `"sandbox":"on"` ve `"env":"sandbox"` yazar.

### 7. Sayılı sınırlar

1. **Disk — sandbox'ın Postgres'i prod'unkiyle aynı diski paylaşır ve boyutu SINIRSIZDIR.** İkisi de
   `local-path` sınıfındadır (sandbox `5Gi`, prod `20Gi` ister) ve tek düğümlü kümede aynı dosya
   sistemine yazar. `local-path` provisioner'ı PVC boyutunu **uygulamaz**: `5Gi` bir istek etiketidir,
   bir tavan değil, ve `02-quota.yaml`'ın `requests.storage: 10Gi`'ı yalnız istekleri sayar, kullanımı
   değil. Bir `tt_test_` anahtarı simülasyonla `transactions`, `audit_log` ve akış satırı ürettikçe
   sandbox'ın veritabanı düğümün diski dolana kadar büyüyebilir — ve dolan disk **prod'un** Postgres'ini
   de durdurur. Oran sınırı büyümeyi yavaşlatır, durdurmaz. Bu bir kabul edilmiş, **sayılı** sınırdır;
   kapatılmadı. **İzleme önerisi** (kümeye bu iş dokunmadı): düğüm dosya sisteminin doluluğuna bir
   uyarı (SigNoz, ör. %80) ve sandbox veritabanının boyutuna haftalık bir bakış:

   ```bash
   kubectl --context hetzner-k8s-1 -n tappa-sandbox exec statefulset/tappa-postgres -- psql -U tappa_owner -d tappa -Atc "SELECT pg_size_pretty(pg_database_size('tappa'))"
   ```

   Kalıcı çare ayrı bir iştir: sandbox'a ayrı bir disk/birim, boyut uygulayan bir depolama sınıfı (ör.
   XFS proje kotası) ya da ayrı bir düğüm.
2. **Havuz — her simülasyon tek bağlantı tutar** (API-11 düzeltme turu 1). Simüle dokunuşun tamamı
   `/v1` yazma koşucusunun tek işleminde koşar; eşzamanlı simülasyon sayısı havuzu aşarsa fazlası havuzda
   **sıra bekler**, kilitlenmez (ölçüm ADR 0028'in API-11 notunda).

## Operator surface (M10 OP-7) — `tappa_operator`'ın girişi ve dört anahtar

**Ne:** platform operatörünün yüzeyi (ADR 0020/0021) **dört değişkenle, tek küme olarak**
açılır: `TAPPA_OPERATOR_DATABASE_URL` (`tappa_operator`'ın DSN'i), `TAPPA_OPERATOR_TOTP_KEK`,
`TAPPA_OPERATOR_TOKEN_HMAC_KEY` (ikisi 32 bayt, base64) ve `TAPPA_OPERATOR_HOST`
(ör. `ops.taptime.mt`). **Hiçbiri yoksa** yüzey kapalıdır: `/operator` **503** döner,
müşteri ürünü etkilenmez — bugünkü durum budur. **Bir kısmı varsa** süreç açılmayı reddeder
(`config.Load`, eksikleri adıyla söyler; değer basmaz). Dördü de `deploy/k8s/20-app.yaml`'da
`tappa-secrets`'tan `optional: true` ile okunur — Secret'ta yokken pod yine kalkar
(`TestPackaging_TheOperatorSurfaceIsOneOptionalSecretSet`).

**OP-7'de yapılandırılmış yüzey HİÇBİR ROTA SUNMAZ** (`/operator` → router'ın kendi 404'ü):
giriş ekranları, oturum kapısı ve host kapısı OP-8'in işidir. Bu bölüm OP-7 birleştikten
sonra her an koşulabilir, ama **önerilen zaman OP-8 ile birliktedir** — o güne kadar
yapılandırmak yalnız bir bağlantı havuzu ve bir başlangıç satırı kazandırır.

### 🔴 Sıra: üç adım, bu sırayla

1. **"Operator roles (M10 OP-5)" bölümü** — `main`'e birleştirmeden ÖNCE (00026 rolleri ister;
   o bölümün kendi gerekçesi).
2. **`main`'e birleştirme** → deploy 00026'yı ve OP-7'nin ikilisini uygular. Dört anahtar
   henüz yok: yüzey **kapalı** açılır. Doğrula (aşağıdaki *Doğrulama*, "önce" satırı):
   başlangıç log'unda `"operator_surface":"off"` ve `/operator` → 503.
3. **Bu bölüm** — `tappa_operator`'a giriş + dört anahtar + rollout. 2. adımdan önce
   koşulursa 00026 yoktur ve operatör havuzu açılamaz.

Her `kubectl` satırı **`--context hetzner-k8s-1 -n tappa`** taşır. Kullanıcı, veritabanı ve
sahibin parolası Postgres konteynerinin kendi ortamından okunur (tek tırnaklı `sh -c`).
**Hiçbir değer ekrana, bir dosyaya ya da bir komut satırına yazılmaz:** değerler yerel
kabuğun değişkenlerinde doğar, `printf` (bash ve zsh'te kabuğun **yerleşik** komutu — bir
süreç argümanı olmaz) ile stdin'e verilir, ve sonunda `unset` edilir. `tappa-secrets`'ın
hiçbir **değeri** ekrana gelmez: tek okuması 3a'daki `kubectl describe`'dır ve o yalnız
anahtar adını ve bayt boyunu basar (bu dosyanın §4.7 kuralı: üzerinde `jsonpath` yok).

### 0) Secret'ı kim yönetiyor — ölç

```bash
kubectl --context hetzner-k8s-1 -n tappa get externalsecret
```

"No resources found" → düz Secret yolu (aşağıda 3a). tappa-secrets listeleniyorsa → external-secrets
yönetiyor (3b): Secret'a doğrudan yazılan anahtarları bir sonraki eşitleme SİLER.

### 1) Değerleri üret — yerel kabukta, ekrana basmadan

- `OP_PW="$(openssl rand -hex 32)"`: tappa_operator'ın parolası (hex: URI'de ayırıcı yok)
- `OP_TOTP_SEAL="$(openssl rand -base64 32)"`: TAPPA_OPERATOR_TOTP_KEK
- `OP_SESSION_SIGN="$(openssl rand -base64 32)"`: TAPPA_OPERATOR_TOKEN_HMAC_KEY
- `OP_HOST="ops.taptime.mt"`: TAPPA_OPERATOR_HOST — sır değil

```bash
umask 077
OP_PW="$(openssl rand -hex 32)"
OP_TOTP_SEAL="$(openssl rand -base64 32)"
OP_SESSION_SIGN="$(openssl rand -base64 32)"
OP_HOST="ops.taptime.mt"
OP_DSN="postgres://tappa_operator:${OP_PW}@tappa-postgres:5432/tappa?sslmode=disable"
```

`config.Load` iki anahtarı birbirine ve öteki her anahtara (`TAPPA_TAG_KEK`, oturum ve davet
anahtarları) karşı sabit zamanda karşılaştırır; eşitse açılmaz. Host küçük harfli bir DNS
adı olmalı (şema, port, yol yok) ve `TAPPA_BASE_URL`'in host'u **olmamalı**.

### 2) `tappa_operator`'a giriş ver — parola psql'in `\password`'üyle, stdin'den

```bash
{ printf 'ALTER ROLE tappa_operator LOGIN;\n\\password tappa_operator\n'
  printf '%s\n%s\n' "$OP_PW" "$OP_PW"; } \
  | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -1 -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

beklenen: ALTER ROLE · iki parola istemi (değer yankılanmaz) · çıkış 0

**Neden `\password`:** psql parolayı **istemcide** SCRAM doğrulayıcısına çevirir ve sunucuya
yalnız onu gönderir (psql belgesi: *"the new password does not appear in cleartext in the
command history, the server log, or elsewhere"*); düz `ALTER ROLE … PASSWORD '…'` ise başarısız
olursa `log_min_error_statement = error` ile parolayı **düz metin** sunucu log'una yazar.
**Neden stdin:** `kubectl exec -i` (`-t` yok) konteynere TTY vermez, psql parola istemini o
zaman stdin'den okur — ölçüldü (geliştirme DB'si, `docker exec -i`, `BEGIN … ROLLBACK` içinde,
2026-10-01): iki istem, `rolcanlogin = t`, saklanan değer bir SCRAM doğrulayıcısı; geri
almadan sonra rol yine NOLOGIN ve parolasız. `-1` stdin betiğini tek transaction'a sarar
(aynı ölçüm: iki ifade aynı `pg_current_xact_id()`).

Doğrula (değer basmaz):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT rolcanlogin, starts_with(rolpassword, 'SCRAM'),
       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid)
  FROM pg_authid r WHERE r.rolname = 'tappa_operator';
SQL
```

beklenen: t|t|0

### 3a) Dört anahtarı `tappa-secrets`'a ekle — düz Secret yolu, TEK yazma

```bash
printf '{"stringData":{"TAPPA_OPERATOR_DATABASE_URL":"%s","TAPPA_OPERATOR_TOTP_KEK":"%s","TAPPA_OPERATOR_TOKEN_HMAC_KEY":"%s","TAPPA_OPERATOR_HOST":"%s"}}' \
    "$OP_DSN" "$OP_TOTP_SEAL" "$OP_SESSION_SIGN" "$OP_HOST" \
  | kubectl --context hetzner-k8s-1 -n tappa patch secret tappa-secrets --type merge --patch-file /dev/stdin
```

beklenen: secret/tappa-secrets patched (kubectl'in --patch-file bayrağı gerekir)

Yalnız ADLAR ve BOYUTLAR — değer basılmaz:

```bash
kubectl --context hetzner-k8s-1 -n tappa describe secret tappa-secrets | grep TAPPA_OPERATOR_
```

beklenen dört satır; iki anahtar 44 bayt (32 baytın base64'ü), host 14 bayt (ops.taptime.mt)

🔴 **Dördü TEK yamada.** Secret'ın değişmesi çalışan pod'u yeniden başlatmaz, ama arada bir
yeniden başlama (node, tahliye) olursa pod **yarım küme** görür ve açılmayı reddeder. Bu
yüzden ayrı ayrı `patch` yok.

### 3b) external-secrets yolu (Secret'ı bir `ExternalSecret` yönetiyorsa)

Dört değeri Infisical'da `/tappa/` altına aynı adlarla gir. Değer ekrana basılmaz; panoyla
verilir ve **her yapıştırmadan hemen sonra pano boşaltılır**:

- `printf '%s' "$OP_DSN" | pbcopy`: Infisical'a yapıştır: TAPPA_OPERATOR_DATABASE_URL
- `pbcopy </dev/null`: panoyu hemen boşalt
- `printf '%s' "$OP_TOTP_SEAL" | pbcopy`: TAPPA_OPERATOR_TOTP_KEK
- `printf '%s' "$OP_SESSION_SIGN" | pbcopy`: TAPPA_OPERATOR_TOKEN_HMAC_KEY

```bash
printf '%s' "$OP_DSN" | pbcopy
pbcopy </dev/null
printf '%s' "$OP_TOTP_SEAL" | pbcopy
pbcopy </dev/null
printf '%s' "$OP_SESSION_SIGN" | pbcopy
pbcopy </dev/null
```

TAPPA_OPERATOR_HOST sır değil: elle yaz (ops.taptime.mt)

**Neden boşaltma:** macOS panosu oturumdaki her uygulamanın okuyabildiği ortak bir alandır;
Evrensel Pano açıksa aynı Apple hesabının öteki cihazlarına da gider. `pbcopy </dev/null`
panoyu boş bir değerle değiştirir, yani değer yalnız yapıştırma ile boşaltma arasındaki
saniyelerde panodadır. Bir pano yöneticisinin o arada aldığı kopyayı **silmez** — öyle bir
araç açıksa bu adımdan önce duraklat. (`printf` kabuğun yerleşiğidir: değer bir süreç
argümanı olmaz.)

Sonra canlı `ExternalSecret`'in `data:` listesine
dört girdiyi **birlikte** ekle (`deploy/examples/externalsecret.example.yaml`'da yorumda
duruyorlar) ve uygula; `kubectl --context hetzner-k8s-1 -n tappa get externalsecret
tappa-secrets` → `SecretSynced`. ⚠️ `ExternalSecret`'te anahtar başına `optional` yoktur:
uzak değerlerden biri eksikse **bütün** eşitleme durur ve öteki anahtarlar da donar —
dördünü Infisical'a girmeden girdileri açma.

### 4) Emanet

`TAPPA_OPERATOR_TOTP_KEK` kaybolursa her operatörün TOTP zarfı açılamaz: geri dönüş her
operatör için `opadmin reset-mfa`'dır (OP-9) — kalıcı değil ama zahmetli; değeri
`TAPPA_TAG_KEK`'in emanetiyle aynı yere (Infisical / parola yöneticisi) koy, **ayrı bir
kayıt** olarak. `TAPPA_OPERATOR_TOKEN_HMAC_KEY` kaybı yalnız açık operatör oturumlarını
düşürür; parola kaybı yeni bir parola + 2. ve 3. adımdır.

```bash
unset OP_PW OP_TOTP_SEAL OP_SESSION_SIGN OP_HOST OP_DSN
```

### 5) Rollout ve doğrulama

```bash
kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa
kubectl --context hetzner-k8s-1 -n tappa rollout status deployment/tappa --timeout=180s
```

**Sonra deploy'u yeniden koştur (T119).** Operatör host'unun iki Ingress'ini (`41-operator-ingress.yaml`)
`deploy.yml` yalnız `tappa-secrets`'ta `TAPPA_OPERATOR_HOST` 0 bayttan büyükken ve rollout
beklendikten sonra uygular; bu adıma kadarki her deploy *"the operator surface is off"* notunu basıp
41'i atlamıştır (taze kümede doğru olan budur). Yani yüzey ilk kez açılıyorsa `ops.taptime.mt` ancak
bir sonraki deploy'da yönlendirilir:

```bash
gh workflow run deploy.yml --ref main
```

beklenen: "Preflight — the operator host is routed only to a server that gates it" adımı
`operator-surface: on` basar, "Route the operator and API hosts" 41'i uygular, "Gate — the operator host must not
serve the customer product" `303 to /operator ... (try N)` basar -- 41 yeni yaratıldığı için ilk
denemeler 404 görebilir (denetleyici yeni Ingress'i eşzamansız yapılandırır); yoklama 404'ü ~90 sn
boyunca yeniden dener, sonra kırmızı biter

**Doğrulama — başlangıç satırı.** Her süreç tam olarak biri olan bir olgu yazar (yokluğu
okumamak için; KEK döndürme kapısının gerekçesi):

"önce" (birleştirmeden sonra, bu bölümden önce) — 1 olmalı:

```bash
kubectl --context hetzner-k8s-1 -n tappa logs deployment/tappa | grep -c '"operator_surface":"off"'
```

"sonra" (bu bölümün 5. adımından sonra) — 1 olmalı:

```bash
kubectl --context hetzner-k8s-1 -n tappa logs deployment/tappa | grep -c '"operator_surface":"configured"'
```

1 ise DUR — yapılandırma tam ama veritabanına ULAŞILAMADI (ağ, ad çözümü, zaman aşımı, kimlik
doğrulama, veritabanı yok, sunucu dolu ya da kapanıyor/açılıyor):

```bash
kubectl --context hetzner-k8s-1 -n tappa logs deployment/tappa | grep -c '"operator_surface":"unavailable"'
```

Başlangıç satırı her süreçte **bir kez**, açılışta yazılır: bu sorguları rollout'tan hemen
sonra koş (log döndürülmüşse satır gitmiş olabilir — o zaman cevap "bilinmiyor"dur, "kapalı"
değil). Yapılandırılmış yüzeyin veritabanına bağlı olduğunun kanıtı `configured` satırının
KENDİSİDİR: o satır ancak havuz açılıp rol kapısı ve log parametresi geri okuması geçtikten
sonra yazılır.

`unavailable` satırı `err` alanında yalnız bir **kod** taşır (ör. `SQLSTATE 28P01` = yanlış
parola, `3D000` = veritabanı yok, `connection refused`) — DSN'i, parolayı, sunucunun mesajını
ya da anahtarı asla (`TestOpenOperatorSurface_PrintsNoValue`). "Ulaşılamaz" sayılanlar KAPALI
bir listedir (`internal/db` → `unreachableSQLSTATEs`: SQLSTATE sınıfı 08 ve 28, `3D000`,
`53300`, `57P01`–`57P03`; ağ, ad çözümü, zaman aşımı). Bu durumda süreç **açıktır** ve müşteri
ürünü servis verir; yalnız `/operator` 503'tür. Nedeni düzelt (çoğunlukla 2. adımla 3. adımın
parolası uyuşmuyor: yeni bir `OP_PW` ile ikisini birlikte yinele), sonra yeniden rollout.

**Doğrulama — yanıt:**

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/operator
```

beklenen: önce 503, sonra 404 -- OP-8'den beri yapılandırılmış yüzey operatör host'unda
(TAPPA_OPERATOR_HOST) cevap verir; müşteri host'unda /operator, router'ın kendi 404'üdür (iki yönlü
host kapısı, ADR 0020 §4 OP-8 notu).

(OP-8: operatör host'unun kendisi — `https://ops.taptime.mt/operator/login` — `40-ingress.yaml`
ile **erişilemez**: o dosya bu host için kural taşımaz. Host'un iki Ingress'i
`41-operator-ingress.yaml`'dadır — 2026-10-09'dan beri canlıda (elle), T112'den beri ağaçta ve
`deploy.yml`'de; bkz. *"Uygulandı: K2"* bölümü. K4: IP kısıtı **yok** (karar 2026-10-14). ⚠️ Yüzey
kapalıyken o Ingress'ler `ops.taptime.mt`'de müşteri ürününü açar; T119'dan beri deploy 41'i yalnız
yüzey yapılandırılmışken uygular ve nesneler yüzeyden uzun yaşıyorsa hiçbir şey uygulamadan kırmızı
biter: aşağıdaki *"Geri alma"*.)

(`pg_stat_activity`'de bir `tappa_operator` bağlantısı aramak bir kanıt DEĞİLDİR: havuz
boştaki bağlantıyı en çok ~30 dk tutar — pgxpool `MaxConnIdleTime` — ve `MinConns` 0'dır,
yani sağlıklı bir süreçte de sonradan sıfır görünür.)

**Açılış reddi** (rollout `status` zaman aşımına uğrar, eski pod servis vermeye devam eder —
`maxUnavailable: 0`): yeni pod'un log'unda `"msg":"fatal"` satırı nedeni adıyla söyler —
*"only some are set"* (yarım küme), *"must differ"* (eşit anahtar), *"TAPPA_OPERATOR_HOST"*
(host biçimi), *"refusing to open"* (DSN `tappa_operator` olarak bağlanmıyor ya da rolün
nitelikleri ADR 0021 §1'e uymuyor — ör. sahibin DSN'i yapıştırılmış), *"the server refused
the connection (SQLSTATE …)"* (sunucuya ulaşıldı ve DSN'in istediğini reddetti: `42501` bir
rol ya da ayar yetkisi, `22023` olmayan bir rol, `42704` bilinmeyen bir ayar — DSN'deki fazla
parametreye bak), *"asks for default_query_exec_mode=…"* (DSN, pgx'in argümanları SQL metnine istemci
tarafında gömdüğü bir kip istiyor — `simple_protocol`; parametreyi kaldır — bugünkü kodda
iki havuz da yalnız bağlı parametre gönderen kipleri kabul eder, ölçen:
`TestPin_ADSNCannotChooseClientSideInterpolation`) ya da *"the connection's configuration
(the … pool) is in query exec mode …"* (aynı kip DSN'den değil KODDAN gelmiş — DSN
denetlenmişti; bir hata raporu konusudur, işletme ayarı değil), *"the connection attempt failed (…), which is not one of the failures that
count as unreachable"* (sunucu cevabı olmayan bir ret: bir TLS uyarısı, bu tarafın kabul
etmediği bir sertifika, TLS'i reddeden bir sunucu ya da tanınmayan bir hata — DSN'in
`sslmode`'una bak; runbook'un DSN'i `sslmode=disable` taşır), ya da
`log_parameter_max_length_on_error` (bağlantı başlangıç parametresi sunucuya ulaşmadı, ya da
geri okuması koşamadı). Bir DSN birden çok deneme doğurursa (çok host'lu DSN; `sslmode=prefer`
ya da hiç `sslmode` yoksa önce TLS sonra düz) "ulaşılamaz" ancak **her** deneme ulaşılamazsa
geçerlidir; ret satırı karar veren denemeyi *"attempt N of M"* diye, ulaşılamaz satırı her
denemenin kodunu sırayla (*"attempt 1: connection refused; attempt 2: SQLSTATE 28P01"*)
adlandırır. Bu yüzden TLS'siz bir sunucuya `sslmode=prefer` ile **listedeki her sunucu cevabı**
— yanlış parola (`28P01`), olmayan veritabanı (`3D000`), açılan ya da dolu sunucu (`57P03`,
`53300`) — **açılışı durdurur** (ilk deneme TLS reddidir); ulaşılamaz kalan yalnız her
denemede ağ düzeyinde olan hatadır (ör. kapalı port). `sslmode=disable` ile aynı cevaplar
yüzeyi `unavailable` bırakır.

### Geri alma — yüzeyi kapat

🔴 **Operatör host'unun iki Ingress'i yüzeyle birlikte gider (T112; sıra T119'dan beri).** Dört
anahtar gidince uygulama operatör-host kapısını (`operatorHostOnly`) **bağlamaz**;
`deploy/k8s/41-operator-ingress.yaml`'ın nesneleri ise `ops.taptime.mt`'yi hâlâ aynı Service'e
yönlendirir — o host müşteri ürününün ikinci, kapısız bir origin'i olur (gerekçe: *"Uygulandı: K2"*
bölümü). **T119'dan beri commit gerekmez:** `deploy.yml` 41'i yalnız `tappa-secrets`'ta
`TAPPA_OPERATOR_HOST` 0 bayttan büyükken uygular, ve anahtar yokken nesneler duruyorsa
**hiçbir şey uygulamadan** kırmızı biter (`scripts/verify-deployment.sh operator-surface`; adım
*"Preflight — the operator host is routed only to a server that gates it"*). Nesneleri yine
**cluster-admin** siler — deploy kimliğinin `ingresses` üzerinde `delete` yetkisi yok
(`01-rbac.yaml`). Sıra bu yüzden **yama → sil → yeniden başlat**:

(1) dört anahtarı tek yamada kaldır — **`rollout restart` HENÜZ YOK.** Çalışan pod'lar ortamlarını
açılışta okudu, kapıyı taşımaya devam ederler; bu arada koşan bir deploy anahtarın gittiğini ve
nesnelerin durduğunu görür ve rollout'tan **önce** kırmızı biter, yani kapılı pod'lar kapısız
olanlarla değişmez:

```bash
kubectl --context hetzner-k8s-1 -n tappa patch secret tappa-secrets --type json -p '[
  {"op":"remove","path":"/data/TAPPA_OPERATOR_DATABASE_URL"},
  {"op":"remove","path":"/data/TAPPA_OPERATOR_TOTP_KEK"},
  {"op":"remove","path":"/data/TAPPA_OPERATOR_TOKEN_HMAC_KEY"},
  {"op":"remove","path":"/data/TAPPA_OPERATOR_HOST"}]'
```

(2) iki nesneyi sil:

```bash
kubectl --context hetzner-k8s-1 -n tappa delete ingress tappa-operator tappa-operator-legal
```

beklenen: iki satır deleted. Host artık uygulamaya ulaşmaz; kümenin varsayılan sunucusu cevaplar --
ölçüldü 2026-10-14, hiçbir Ingress'in yönlendirmediği bir ad için (curl --resolve ile origin'e,
salt-okur): `/` -> 404 text/html, `/healthz` -> 200 boş gövde (denetleyicinin kendi /healthz'i; bu
yüzden T119'un yoklaması /healthz'e değil `/`'ye bakar)

`ops-taptime-tls` sırrı kalır; cert-manager'ın bu Ingress'ten türettiği `Certificate`'ın silinmeyle
gidip gitmediği ölçülmedi. (3) pod'ları yeniden başlat ve rolün girişini kapat:

```bash
kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "ALTER ROLE tappa_operator NOLOGIN PASSWORD NULL"'
```

Acil durumda (2) (1)'den önce gelebilir; ama o aralıkta koşan bir deploy anahtarı hâlâ görür ve
nesneleri **yeniden yaratır** (41'i uygular) — o zaman (2)'yi (1)'den sonra yinele. (1)–(3)'ten
sonraki deploy 41'i atlar (*"the operator surface is off"* notu) ve rollout'tan sonraki yoklama
`ops.taptime.mt` için `404 -- not routed to the application, as the surface is off` basar.

Dördü yine **tek** yamada (yarım küme yok); sonra başlangıç satırı `"operator_surface":"off"`.

**3b yolunda (Secret'ı bir `ExternalSecret` yönetiyorsa) yukarıdaki `patch` YETMEZ:** bir
sonraki eşitleme (`refreshInterval: 1h`, ya da `ExternalSecret`'e dokunan ilk değişiklik)
silinen anahtarları Secret'a geri yazar ve yüzey bir sonraki yeniden başlamada geri açılır.
Sıra: önce canlı `ExternalSecret`'in `data:` listesinden dört `TAPPA_OPERATOR_*` girdisini
**birlikte** kaldır ve uygula, eşitlemeyi zorla, Secret'ta kalmadığını doğrula (bu, yukarıdaki
(1)'in yerini alır); **ondan sonra** (2) iki Ingress'i sil ve (3) `rollout restart` ve `NOLOGIN`
satırları:

(canlı ExternalSecret manifestinden dört girdi kaldırılıp uygulandıktan sonra)

```bash
kubectl --context hetzner-k8s-1 -n tappa annotate externalsecret tappa-secrets force-sync="$(date +%s)" --overwrite
kubectl --context hetzner-k8s-1 -n tappa get externalsecret tappa-secrets
```

beklenen: SecretSynced

```bash
kubectl --context hetzner-k8s-1 -n tappa describe secret tappa-secrets | grep -c TAPPA_OPERATOR_
```

beklenen: 0 — yalnız ad sayar, değer basmaz

`creationPolicy: Owner` ile (örnek dosyadaki) eşitleme Secret'ın verisini `ExternalSecret`'in
listesinden yeniden kurar, yani kaldırılan girdi Secret'tan da düşer; `0` görmüyorsan (ör.
`Merge` politikası) yukarıdaki `patch`'i de koş ve yeniden say. Infisical'daki değerler emanet
olarak kalabilir: `ExternalSecret` onları artık istemez. Bu davranış external-secrets'ın
belgesine dayanır, bu repoda **ölçülmedi** (ajan kümeye dokunmaz).

### ⚠️ Geri yüklemeden sonra (B YOLU, taze küme)

Rol parolaları bir veritabanı dökümüne **girmez** ve `01-roles.sql` `tappa_operator`'ı
yeniden **NOLOGIN** yaratır. Emanetten geri gelen `tappa-secrets` dört anahtarı taşıyorsa
süreç yine **açılır** ama yüzey `unavailable` olur (28P01) — müşteri ürünü etkilenmez. Yeni
bir `OP_PW` ile 1., 2. ve 3. adımı (DSN'i yeni parolayla) yinele ve rollout et.

### Geliştirme

Taze bir geliştirme veritabanı (`make db-reset`, CI) `tappa_operator`'a geliştirme parolasını
`scripts/db-init/02-dev-only-password.sh`'den alır. **Halihazırda çalışan** geliştirme
veritabanında bir kez:

```bash
docker exec -i tappa-db sh -c 'psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
ALTER ROLE tappa_operator LOGIN PASSWORD 'tappa';
SQL
```

ve `.env`'e dördü birlikte (`.env.example`'daki yorumlu blok): DSN
`tappa_operator`/`tappa` ile `localhost`, iki anahtar `openssl rand -base64 32` ile
**ayrı ayrı**, host `ops.localhost` (`localhost` olamaz: `TAPPA_BASE_URL`'in host'u).

---

## Operator accounts (M10 OP-9) — `cmd/opadmin`

**Ne:** platform operatörü hesabı `cmd/opadmin`'in ürettiği SQL ile doğar, sıfırlanır ve
kapanır (ADR 0020 §6). 00026 `platform_admins` üzerinde INSERT'i uygulama rollerine ve
`tappa_operator`'a vermez (katalog pinleri: `TestOperator00026_PrivilegeMatrix`,
`TestOperator00026_AppHoldsNothingUnderTheProductionDefaultACL`), SQL'i sahip uygular.
`opadmin` bir **filtredir** (`cmd/rotatekek` emsali): bağımlılık kapanışı standart kütüphane +
komutun kendisi, içinde `database/sql`, `net`, `os/exec` yok, ve sayılı ortam çağrıları
kaynakta yok (ölçen: `TestDeps_NoDriverInTheClosure`,
`TestDeps_NoEnvironmentAndNoDSNName`) — SQL'i stdout'a yazar, sen onu `psql` ile
**`tappa_owner`** olarak uygularsın.

| Alt komut | Ne yapar | Link |
|---|---|---|
| `create --email ADRES --name "AD" --host OPS_HOST` | yeni `pending` hesap; id'yi opadmin üretir | evet |
| `reset-mfa --id ID --email ADRES --host OPS_HOST` | TOTP zarfı ve parola özeti silinir, kilit sayacı sıfırlanır, hesap `pending`'e döner, açık oturumları iptal edilir | evet |
| `disable --id ID --email ADRES` | hesap `disabled`, açık oturumları iptal edilir | hayır |

**Kurallar (hepsi kodda; adı geçen testler ölçer):**

- **Link biçimi** (OP-8'in sayfası okur): `https://<OPS_HOST>/operator/enroll?id=<hesap id>#<link sırrı>`.
  Hesap id'si sorguda (bir kimlik bilgisi değil), sır **fragment**'ta. Link **bir kez**
  kullanılır ve SQL'in **uygulandığı** andan itibaren **30 dk** geçerlidir (veritabanının
  saati; `TestCreate_AnExpiredLinkIsRefused`).
- **Link stderr'e bir kez** yazılır; SQL'de link sırrının SHA-256'sı vardır, kendisi yoktur
  (ölçen: `TestLeak_TheSecretIsOnlyInTheLinkOnStderr` — stdout ve varsayılan logger'lar).
  `create` ve `reset-mfa` stderr bir terminal değilse **çalışmaz** (yönlendirilmiş bir stderr
  linki bir dosyaya ya da log'a koyardı). Denetim dosya kipine bakar: kendini kaydeden bir terminal (`script`, tmux
  `pipe-pane`, terminal oturum kaydı, ekran kaydı) da geçer — o sırada bunları kapat (O9-2).
- **`--host`** `TAPPA_OPERATOR_HOST`'un değeridir, aynı kuralla: küçük harfli DNS adı;
  şema, port, yol yok (`TestHost_IsConfigsRule`).
- **Hedef iki anahtarla:** `reset-mfa` ve `disable` hesabı `--id` **ve** `--email` ile
  bulur; ikisi aynı satırı göstermiyorsa SQL reddeder ve transaction geri alınır (yanlış
  kopyalanmış bir id başka bir operatörü sıfırlamasın diye;
  `TestApply_ASecondCreateAndAMismatchedDisableAreRefused`,
  `TestResetMFA_RefusesWhatItMustNot`). Hesap id'si: `SELECT id, email, status FROM
  platform_admins ORDER BY created_at;`. E-posta büyük/küçük harf duyarsız eşleşir.
- **SQL tek transaction:** `BEGIN` · iki `SET LOCAL` · geçici `pg_temp.opadmin_in` tablosu ·
  `COPY … FROM STDIN` ve **tek veri satırı** · tek bir `DO` bloğu · `COMMIT`. Link sırrının
  SHA-256'sı, e-posta ve ad **ifade metninde değil, COPY veri satırında** hex olarak gider
  (2. tur): sunucu log'u ifade **metnini** yazar (`log_statement=all` her ifadeyi —
  geliştirme DB'si böyle koşar —, `log_min_error_statement=error` başarısız ifadeyi —
  üretimin varsayılanı), COPY verisi ifade metni değildir. Ölçüldü (psql 18.4, kayıt yapan
  bir aktarıcı üzerinden, ölçülen yollarda; dosya stdin'de, `-f` ile ve `\i` ile): psql'in
  gönderdiği ifadelerde bu üç değerden hiçbiri yoktu; sunucuya yalnız tek COPY veri
  mesajında ulaştılar. 🔴 **Yalnız psql dosyayı okurken: `< dosya` ya da `-f dosya`.**
  `psql -c "$(cat dosya)"` dosyayı **tek ifade** olarak gönderir (ölçüldü: 1 ifade, hash'i
  taşıyor, `\` için sözdizimi hatası — başarısız ifadenin metni log'a); bir GUI sorgu aracı
  aynı sınıftır (ölçülmedi). Veri satırında değerlerden önce 100 baytlık sabit bir dolgu
  alanı durur: bir COPY hatası satırın ilk 100 baytını CONTEXT'e yazar (3. tur, aşağıda
  O9-3). İfade metninde kalanlar: sabitler, hesap id'si (tırnak içinde, kanonik uuid
  biçimine doğrulanmış), dolgu ve üretim zamanı. Okunur hâller stderr raporunda ve
  uygulamadaki `NOTICE` satırındadır. Kısıt reddi kısıtın **adıyla** döner, satırı
  yankılamadan.
- **Audit satırı (M10 OP-14 D, migration 00033):** `DO` bloğu hesabın işini yaptığı aynı
  ifadede eylemin **tek** `operator_audit_log` satırını yazar — `create` → `operator_created`,
  `reset-mfa` → `operator_mfa_reset`, `disable` → `operator_disabled` (her uygulama bir satır;
  ikinci `disable` ikinci satırı yazar). Satır hesabı **id'siyle** adlandırır, başka hiçbir şey
  taşımaz (oturum, aktör, tenant, kapsam, sayfa yok; `detail` boş — adres ve ad audit'e girmez;
  şema bunu `actor_shape`'in üçüncü koluyla zorlar). İş ve iz birlikte commit edilir ya da
  hiçbiri; reddedilen bir uygulama satır bırakmaz (`TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone`,
  `TestApply_AFailureAnywhereLeavesNoRow`). Satırın zamanını betik yazmaz: veritabanının
  tetikleyicisi her satıra duvar saatini basar, sahibin satırına da. Satır operatör yüzeyinde
  `/operator/audit`'te *"By the platform owner, with opadmin"* diye görünür. **Uygulama
  adımları ve beklenen psql çıktısı değişmedi** (yeni ifade yok, `NOTICE` aynı); **Doğrula**
  adımına bir sorgu eklendi: hesabın son opadmin audit satırı (`kind`, `at`) — aşağıda.
  🔴 **Sıra:** bu betik veritabanının **00033'te** olmasını ister — `main`'deki deploy'un
  migrate Job'u onu uygular. 00033'ü görmemiş bir veritabanında betik `refused by constraint
  operator_audit_log_actor_shape (SQLSTATE 23514). Nothing was changed.` ile reddedilir ve hiçbir
  şey değişmez (ölçen: `TestOperator00033_DownGivesBack00032AndUpTakesItAgain`); önce deploy,
  sonra opadmin. Ters yön reddedilmez: 00033'ten sonra yalnız bu commit'in ya da sonrasının
  opadmin'i kullanılır; eski bir betik iz bırakmaz (Doğrula'nın altındaki not).
- **Üretim zamanı (2. ve 3. tur):** betik, üretildiği andaki makine saatini taşır.
  `create`/`reset-mfa` **üretimden 30 dk sonra** ve üretim zamanı veritabanı saatinin
  **önündeyse** (3. tur: kodda pay yok — ölçülen en küçük ret +250 ms, daha küçüğü ölçülmedi;
  2. turun 30 sn payı ölçülmüş bir tekrar penceresiydi)
  reddedilir — ileri bir saatle üretilmiş dosya, veritabanı saati üretim zamanını geçince
  **aynen** yeniden uygulanır. `reset-mfa` ayrıca hesabın son linki bu betik üretildikten
  **sonra** verilmişse reddedilir (A-B-A: R1 uygulanıp kullanılır, R2 uygulanıp
  kullanılır, R1 yeniden uygulanır → red). Ölçenler: `TestScript_GenerationGuards` (iki
  alt komut: 29/31 dk, +250 ms/+1/+20 sn, bekleyip aynı dosya), `TestResetMFA_ABAReplayIsRefused`,
  `TestResetMFA_AClockAheadDoesNotReopenAUsedLink` (saat 2 sn ve 10 dk ileri). Yani:
  **üret, hemen uygula, dosyayı sil.**
- 🔴 **`-X -v ON_ERROR_STOP=1` şart.** Ölçüldü (geliştirme DB'si — sunucu 17.10, istemci
  psql 18.4 — geçici tablolarda): psql varsayılanında bir hata olsa da çıkış **0**'dır —
  transaction geri alınır ama komut "başarılı" görünür; `ON_ERROR_STOP=1` ile hata **çıkış
  3** verir. Reddetme ifadesi (opadmin bir girdiyi reddedince stdout'a yazdığı `DO … RAISE`)
  de varsayılan psql'de çıkış **0**, `ON_ERROR_STOP=1` ile **3** verir. `-X` psqlrc'yi
  atlar: psqlrc'de `ON_ERROR_ROLLBACK=on` varsa psql her ifadeyi bir savepoint'e sarar ve
  `DO` bloğundan **önceki ya da sonraki** bir hata geri sarılır, `DO`'nun yazısı **commit
  edilir** (ölçüldü, çıkış 0, 1 satır). `-1` gerekmez (SQL kendi `BEGIN`'ini taşır);
  yanlışlıkla eklenirse iki `WARNING` basar, sonuç aynıdır.

### create — kurulumun ilk operatörü dahil

Birleştirilmiş commit'in checkout'unda, repo kökünde. Komut satırı adres, ad ve host taşır
— kabuk geçmişine giren satırda sır yok; link programın **çıktısıdır**.

```bash
umask 077
OPDIR="$(mktemp -d)"
```

1) SQL'i üret: stdout dosyaya, stderr TERMİNALDE (rapor + link, bir kez)

```bash
go run ./cmd/opadmin create --email ops@example.com --name "Ada Operator" --host ops.taptime.mt \
    > "$OPDIR/op.sql"
```

beklenen: çıkış 0; stderr'de hesap id'si, adres, ad ve link.

2) Oku: başlıkta hesap id'si; BEGIN … COMMIT; tek DO bloğu.

```bash
less "$OPDIR/op.sql"
```

3) Uygula — tappa_owner, ilk hatada dur; 4) dosyayı sil — uygulanmış bir betik saklanmaz.
İkisi tek blokta: silme, uygulamanın sonucu ne olursa olsun aynı yapıştırmada koşar.

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
    < "$OPDIR/op.sql"
rm -rf "$OPDIR"; unset OPDIR
```

beklenen: BEGIN · SET · SET · CREATE TABLE · COPY 1 · NOTICE: opadmin create: pending operator
account … · DO · COMMIT; çıkış 0

çıkış 3 → reddedildi ve transaction geri alındı; mesaj nedeni adıyla söyler (ör. "refused by
constraint platform_admins_email_key" = bu adres zaten kayıtlı).

**Linki ilet.** Kendi hesabınsa linki doğrudan tarayıcına yapıştır. Başkası içinse geçmiş
tutmayan bir kanaldan (yüz yüze, telefonda okuyarak, kaybolan mesaj) ver; bilet sistemine,
e-postaya, geçmişi saklanan bir sohbete yapıştırma. Sonra terminalin kaydırma geçmişini
temizle (macOS Terminal: ⌘K; genel: `clear && printf '\e[3J'`). **Bu komutların
silmedikleri, adıyla:** tmux/screen'in kendi geçmişi (`tmux clear-history` ayrıca gerekir;
screen'de kaydırma tamponu) · bir pano yöneticisinin tuttuğu kopya (kopyala-yapıştır
yaptıysan) · tarayıcı geçmişinde fragment'lı URL'nin kalıp kalmadığı (OP-8'in sayfası
fragment'ı `history.replaceState` ile adres çubuğundan siler; geçmiş girdisi bu turda
**ölçülmedi**). 30 dk içinde kullanılmazsa: aynı hesap için yeni bir `reset-mfa` yeni bir
link basar ve eskisini geçersiz kılar.

**Doğrula** (yalnız durum; değer basmaz):

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
SELECT id, status, enroll_used_at IS NOT NULL AS used,
       round(extract(epoch FROM enroll_expires_at - clock_timestamp()))::int AS seconds_left
  FROM platform_admins ORDER BY created_at;
SELECT a.id, l.kind, l.at, round(extract(epoch FROM clock_timestamp() - l.at))::int AS seconds_ago
  FROM platform_admins a
  LEFT JOIN LATERAL (SELECT kind, at FROM operator_audit_log
                      WHERE target_admin_id = a.id
                        AND kind IN ('operator_created', 'operator_mfa_reset', 'operator_disabled')
                      ORDER BY at DESC, id DESC LIMIT 1) AS l ON true
 ORDER BY a.created_at;
SQL
```

create'ten sonra: \<id>|pending|f|~1800 · kişi linki kullandıktan sonra: \<id>|active|t|\<sayı>
(kullanımdan sonra enroll_expires_at silinmez: son sütun pozitif kalabilir ve anlamı yoktur — link
"used" sütunu t olduğu için ölüdür)

ikinci sorgu (OP-14 D, 00033) — hesabın son opadmin audit satırı: az önce uyguladığın eylemin türü
ve birkaç saniyelik yaş: \<id>|operator_created|\<zaman>|\<saniye> (reset-mfa → operator_mfa_reset,
disable → operator_disabled). Tür boşsa, başka bir eylemin türüyse ya da yaş eylemden eskiyse betik
İZ BIRAKMADI: 00033'ten önceki bir opadmin'le üretilmiştir (ADR 0021 OP-14 D notu LD11); eksik iz
sonradan yazılamaz (günlük append-only).

(ölçen: TestRunbook_TheVerifyQueryShowsEachActionsRow — bu bloğu README'den okuyup koşar)

**00033'ten sonra yalnız bu commit'in ya da sonrasının opadmin'i kullanılır; eski bir betik iz
bırakmaz.** Bu commit'ten önceki bir opadmin'in betiği 00033'teki veritabanında reddedilmez:
hesabı değiştirir, audit satırı yazmaz (üçüncü gözün ölçümü, `disable` için, geri alınan
işlemde). `disable`'ın üretim zamanı koruması yoktur — eski bir `disable` dosyası süresiz
uygulanabilir; bu yüzden uygulanmış dosya saklanmaz (4. adım) ve her uygulamadan sonra yukarıdaki
ikinci sorgu bakılır.

### reset-mfa — cihaz kaybı ya da süresi geçmiş link

```bash
umask 077; OPDIR="$(mktemp -d)"
go run ./cmd/opadmin reset-mfa --id <HESAP_ID> --email <ADRES> --host ops.taptime.mt > "$OPDIR/op.sql"
```

3. adımdaki kubectl … psql -X -v ON_ERROR_STOP=1 … < "$OPDIR/op.sql"

beklenen NOTICE: "account … was active, is now pending; N live session(s) revoked; …"

```bash
rm -rf "$OPDIR"; unset OPDIR
```

Kişi yeni linkle **yeniden enroll olur** (yeni parola + yeni TOTP anahtarı). Reddedilenler
— transaction geri alınır — ve her birinde ne yapılacağı:

| Ret | Ölçen | Yapılacak |
|---|---|---|
| id ile adres aynı hesabı göstermiyor | `TestResetMFA_RefusesWhatItMustNot` | girdiyi düzelt (yeni betik aynı girdiyle yine reddedilir) |
| hesap `disabled` | `TestResetMFA_RefusesWhatItMustNot` | opadmin'de yolu yok (aşağıda, *disable*) |
| hesabın hash'i zaten bu betiğinki (aynı dosya ardı ardına) | `TestResetMFA_RefusesWhatItMustNot` | betik uygulanmış; gerekiyorsa yeni bir `reset-mfa` üret |
| hesaba bu betik üretildikten sonra link verilmiş | `TestResetMFA_ABAReplayIsRefused`, `TestScript_GenerationGuards` | yeni bir `reset-mfa` üret — makine saati veritabanınınkinin **gerisindeyse** yeni betik de, makine saati son verilişi geçene kadar reddedilir (O9-5) |
| betik 30 dk'dan eski | `TestScript_GenerationGuards` | yeni bir `reset-mfa` üret |
| üretim zamanı veritabanı saatinin önünde | `TestScript_GenerationGuards` | veritabanı saati geçince **aynı dosyayı** yeniden uygula, ya da makinenin saatini düzelt |

`TAPPA_OPERATOR_TOTP_KEK` kaybolursa geri dönüş her operatör için bu adımdır ("Operator
surface (M10 OP-7)" → 4. adım).

### disable — erişimi hemen kes

```bash
umask 077; OPDIR="$(mktemp -d)"
go run ./cmd/opadmin disable --id <HESAP_ID> --email <ADRES> > "$OPDIR/op.sql"
```

3. adımdaki kubectl … psql -X -v ON_ERROR_STOP=1 … < "$OPDIR/op.sql"

beklenen NOTICE: "account … was active, is now disabled; N live session(s) revoked"

```bash
rm -rf "$OPDIR"; unset OPDIR
```

Açık oturumlar aynı transaction'da iptal edilir; parola adımı `disabled` hesaba bilinmeyen
bir adresin yanıtını (`ErrRefused`) verir (`TestDisable_EndsSessionsAndSignIn`). İkinci
uygulamadan sonra durum `disabled`, oturum sayısı ve daha önce iptal edilmiş oturumun
`revoked_at`'i aynıdır. `disable` üretim zamanı taşımaz (link vermez). **Kapatılmış bir
operatörü geri almak opadmin'de yok** (bilinçli: o bir MFA sıfırlaması değil, ayrı bir karar)
— adres `platform_admins`'te kayıtlı kalır, yeni bir `create` aynı adresi reddeder.

🔴 **`ERROR: canceling statement due to lock timeout` (SQLSTATE `55P03`) gördüysen:** betik
hesabın satırını 5 sn içinde kilitleyemedi ve **transaction geri alındı** (rotatekek'in
`lock_timeout` emsali). Bir `tappa_operator` oturumu o satırı tutuyor olabilir: oturum
gerektirmeyen `op_record_auth_event('totp_failed', <aktif adres>)` açık bir transaction'da
çağrılırsa satır kilidi transaction bitene kadar kalır (üçüncü göz ölçtü; DSN sahibi bunu
süresiz tutabilir — sınır O9-4). Acil `disable` için sıra:

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
ALTER ROLE tappa_operator NOLOGIN;
SELECT pid, state, xact_start FROM pg_stat_activity WHERE usename = 'tappa_operator';
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = 'tappa_operator';
SQL
```

sonra disable betiğini yeniden uygula (yukarıdaki 3. adım), ardından:

```text
ALTER ROLE tappa_operator LOGIN;   (parola değişmez; operatör yüzeyinin havuzu yeniden bağlanır)
```

`NOLOGIN` yeni bağlantıyı keser, `pg_terminate_backend` `tappa_operator`'ın açık
bağlantılarını düşürür — operatör yüzeyi o arada hata verir; müşteri ürünü `tappa_app` ile
bağlanır ve sorgular yalnız `usename = 'tappa_operator'` satırlarını seçer. Bu adımlar bu turda
**ölçülmedi** (kalıcı satır bırakmadan kilit tutan bir operatör oturumu kurulamadı).

### Sayılı sınırlar (OP-9)

- **O9-1 · ~~opadmin'in eylemleri `operator_audit_log`'a yazılmaz.~~** *(M10 OP-14 D, 00033:
  **kapandı** — her eylem bir audit satırı yazar, yukarıda "Audit satırı". Aşağıdaki iz 00033'ten
  önceki eylemler için geçerli kalır: append-only günlük geriye dönük doldurulmaz.)* 00026'nın tür kümesi
  kapalıdır (beş oturum öncesi başarısızlık + `login`, `enrollment`, `logout`) ve bir
  sahip eylemi türü yeni bir migration ister — **OP-14'e, adıyla**. Veritabanında kalan
  iz dardır: `create` → `created_at` ve `enroll_issued_at`; `reset-mfa` →
  `enroll_issued_at` (her `reset-mfa` bir öncekinin değerini **ezer**) ve o anda açık
  oturumların `revoked_at`'i; `disable` → yalnız `status` (zaman damgası yok — `disabled_at`
  sütunu yok) ve o anda açık oturum **varsa** onların `revoked_at`'i. Ötesi psql oturumunun
  kendisi. **Kalan (00033'ten sonra), adıyla:** satır "sahip uyguladı" der, **kimin** —
  hangi insanın — uyguladığını söylemez (sahip tek bir veritabanı rolüdür); psql oturumunun
  kendisi yine kayıt dışıdır. Sahip tablonun ve tetikleyici fonksiyonunun sahibidir: betikten
  satırı silip uygulayabilir, ya da tetikleyiciyi kapatarak, düşürerek veya fonksiyonunu
  değiştirerek satırı başka bir zamanla yazabilir (ADR 0021 sınır 5; OP-14 D notu LD2) — iz
  sahibe karşı bir kontrol değil, sahibin kendi eylemlerinin kaydıdır. **Bir hesap artık
  silinemez (00033):** her `create` bir `operator_created` satırı yazar, satır hesabı yabancı
  anahtarla (`ON DELETE RESTRICT`) tutar ve günlük append-only'dir — önceden kullanılmamış,
  yanlış girilmiş bir `pending` hesap silinebiliyordu; şimdi düzeltme yolu `disable` + doğru
  adresle yeni bir `create`'tir (yanlış hesap `disabled` ve adresiyle kayıtlı kalır; aynı adresle
  yeniden `create` `platform_admins_email_key` ile reddedilir — yanlış girilen yalnız ad idiyse o
  adres yeni bir hesaba verilemez; ADR 0021 OP-14 D notu LD9). Bu yol, kapatılmış hesabın
  kullanılmamış linkinin ölü olmasına dayanır: `disable` linki silmez, onu yalnız
  `op_complete_enrollment`'ın `status = 'pending'` koşulu reddeder (ölçen:
  `TestDisable_TheUnusedLinkOfADisabledAccountIsRefused`). **Eski bir opadmin iz bırakmaz:**
  bu commit'ten önceki bir opadmin'in betiği 00033'teki veritabanında hesabı değiştirir, audit
  satırı yazmaz (ADR 0021 OP-14 D notu LD11) — yalnız bu commit'in ya da sonrasının opadmin'i
  kullanılır ve her uygulamadan sonra Doğrula'nın ikinci sorgusuna bakılır.
- **O9-2 · Terminal denetimi dosya kipine bakar:** kendini kaydeden bir terminal de bir
  karakter aygıtıdır ve geçer; `/dev/null` da geçer (link ekrana çıkmaz, atılır).
- **O9-3 · Sunucu log'una ifade metni düşer; o metinde hesap id'si, dolgu, üretim zamanı ve
  sabitler vardır** — link sırrının hash'i, e-posta ve ad **COPY veri satırındadır**
  (yukarıda). Ölçüm yöntemi ve kapsamı: sunucu log'u **okunmadı** (ajan kuralı); ölçülen,
  psql'in sunucuya **gönderdiği** (kayıt yapan aktarıcı; dosya stdin'de, `-f`, `\i`) ve
  sunucunun **döndürdüğü** hata alanlarıdır (ERROR/DETAIL/HINT/CONTEXT — log'un STATEMENT
  yanında yazdığı satırlar); depoda `TestLog_TheValuesReachTheServerOnlyAsCopyData` (yedi ret
  yolu) ve kilit zaman aşımı için `TestApply_ARowHeldElsewhereFailsFastNotForever`. **COPY
  hatasının CONTEXT'i** (3. tur): bir veri hatası ya da COPY sırasında gelen bir **iptal**
  (`pg_cancel_backend`, TTY'li psql'de Ctrl-C, ileride bir `statement_timeout`) satırın ilk
  100 baytını CONTEXT'e yazar — ölçüldü (veri hatası: 100 bayt + `...`; iptal yolu
  PostgreSQL kaynağından, ölçülmedi); o 100 bayt `-- ` ve dolgudur, değerler 104. bayttan
  başlar (`TestCopy_ADataErrorShowsThePaddingNotTheValues`); DO bloğu dolguyu tam eşitlikle
  denetler. Kapsam dışı, adıyla: **dosyayı okumayan istemci kipleri** (`psql -c`, GUI sorgu
  araçları — `-c` ölçüldü: hash STATEMENT satırına düşer) · `auto_explain` ya da
  `pg_stat_statements` yüklü bir sunucu (geliştirme DB'sinde ikisi de yok; üretimin manifesti
  `args: []`).
- **O9-4 · DSN sahibi bir `disable`/`reset-mfa`'yı geciktirebilir:** açık bir transaction'da
  hesabın satırını tutan bir `op_*` çağrısı betiği 5 sn sonra `55P03` ile düşürür; çare
  yukarıdaki `NOLOGIN` + `pg_terminate_backend` adımıdır (ölçülmedi).
- **O9-5 · Üretim zamanı korumaları iki saati karşılaştırır** — hesabın `enroll_issued_at`'i
  ve veritabanının saati ile betiğin üretim zamanı (opadmin'i koşan makinenin saati).
  **Makine ileride:** betik veritabanı saati üretim zamanını geçene kadar reddedilir (aynı
  dosya sonra uygulanır). Bu ret yüzünden kabul edilmiş bir R1'in üretim zamanı kendi
  verilişinden geride ya da eşittir; veritabanı saati geri atmadıkça sonraki bir veriliş ondan
  ileridedir, yani R1'in yeniden uygulanması reddedilir — 2. turun 30 sn'lik penceresi bu
  varsayımla **kapandı** (3. tur; saat
  2 sn ve 10 dk ileriyken ölçüldü: `TestResetMFA_AClockAheadDoesNotReopenAUsedLink`).
  **Makine geride (s kadar):** bir verilişten sonraki s içinde üretilen `reset-mfa`, makine
  saati verilişi geçene kadar reddedilir (üçüncü göz ölçtü: 2 dk geride, `create`'ten sonra
  üretilen reset reddedildi); 30 dk'lık yaş penceresi s kadar kısalır. **Kalan:** veritabanı
  saatinin iki veriliş arasında **geri** atması (NTP düzeltmesi) sonraki verilişe daha erken
  bir damga verebilir — ölçülmedi. Geliştirme makinesi ↔ DB farkı: **~1 ms** (test log'u);
  üretimdeki fark ölçülmedi.

### Uygulandı: K2 — `ops.taptime.mt`'nin iki Ingress'i (2026-10-09 elle; T112 ile repoda) · K4: IP kısıtı yok (karar 2026-10-14)

**Durum.** 2026-10-09'da operatör yüzeyi açılırken bu bölümün o günkü **taslağı** (iki Ingress,
IP kısıtı yok) `kubectl apply` ile **elle** uygulandı; `deploy.yml` onu uygulamadığı için küme
ile ağaç ayrıştı (backlog T112 — T41–T44'ün sınıfı: *"kümede düzeltildi, manifest bilmiyor"*).
Artık iki nesne **`deploy/k8s/41-operator-ingress.yaml`**'dadır ve `deploy.yml`'in *"Roll out
the server"* adımı onu `40-ingress.yaml`'dan hemen sonra uygular — aynı kimlik, aynı fiiller
(`01-rbac.yaml`: `ingresses` → `get, list, create, patch`; **yeni yetki yok**). Pin:
`TestPackaging_TheDeployAppliesEveryIngressFile` (ağaçtaki her Ingress dosyası deploy'da
uygulanır; T112'nin sınıfı Ingress'ler için kapalı). ⚠️ **T119 (2026-10-14):** "hemen sonra"
artık geçerli değil — 41 rollout **beklendikten sonra** ve yalnız yüzey yapılandırılmışken
uygulanır; aşağıdaki *"T119 — üç kapı"*.

Dosya, orkestratörün 2026-10-10'da aldığı canlı dökümle (status, managedFields ve
last-applied atılmış) **alan alan aynıdır**: iki belge PyYAML ile bütün olarak karşılaştırıldı,
ikisi de eşit; pin `TestPackaging_TheOperatorIngressesAreTheLiveObjects`. Canlı nesnelerde
**etiket yoktur**, dosyada da yoktur — `app.kubernetes.io/name: tappa` eklemek ilk deploy'da
canlıyı **değiştirirdi**, yani bir karardır, kopya değil. İlk deploy'da `kubectl apply` bu iki
nesne için `unchanged` (ya da yalnız `last-applied` ek açıklamasını yazan `configured`)
basmalıdır; başka bir alan değişiyorsa dur. YAML burada **ikinci kez yazılmaz**: tek tanım
dosyadır (eskiden buradaki taslak, ağaçtaki tek yazımdı).

Operatör yüzeyi `TAPPA_OPERATOR_HOST`'ta yaşar ve uygulamanın iki yönlü host kapısı (OP-8)
operatör rotalarını müşteri host'larında, müşteri rotalarını operatör host'unda 404'e
çevirir; Ingress'in işi o host'u pod'a getirmektir. **İki Ingress**, çünkü gövde sınırı yola
göre değişir ve ingress-nginx'te `proxy-body-size` gibi ek açıklamalar bir Ingress kaynağının
**bütün** yollarına uygulanır; yol başına farklı bir sınır, o yol için ayrı bir Ingress kaynağı
demektir (ingress-nginx'in ek açıklama modeli; **kümede ölçülmedi** — iki kaynağın aynı host'ta
tek `server` bloğunda birleştiği ve her birinin `location`'ının kendi sınırını taşıdığı,
aşağıdaki salt-okur `nginx -T` komutuyla ölçülecek). Sınırlar, operatör paketinin kendi
sınırlarına AST'den bağlıdır (`TestPackaging_TheOperatorBodyLimitsFollowTheHandlersBounds`:
her gövde okuması `readForm`'dan geçer, `maxLegalBody`'yi yalnız `publishLegal` verir ve o
yalnız `Route(Prefix)` altında `POST /legal`'dır; her sınır kendi bağının üstünde, iki katının
altında; `/`'nin sınırı `maxLegalBody`'nin altında):

- **`tappa-operator`** — `/` (Prefix), **`24k`**. Operatör yüzeyinin `/operator/legal` dışındaki
  her gövde okuması `readForm`'dan `maxFormBytes` = 16 KiB ile geçer (`/operator/login`,
  `/operator/login/totp`, `/operator/enroll` — oturum öncesi yolların üçü; kaynak okundu:
  `internal/handler/operator/render.go`, `signin.go`, `enroll.go`); `/operator/logout` ve
  `GET`'ler gövde okumaz. 24k = 16 KiB + 8 KiB pay: sınırın hemen üstünü nginx'in markasız
  413'ü değil uygulamanın kendi 413 sayfası karşılar, ve oturum öncesi bir istemci nginx'e
  320 KiB tamponlatamaz (tek Ingress taslağının açığı buydu).
- **`tappa-operator-legal`** — yalnız `/operator/legal` (Exact), **`320k`**. Uygulama istek
  gövdesini 262 144 baytta (256 KiB, `maxLegalBody`) keser ve aşanı kendi 413 sayfasıyla ("That
  text is too long") reddeder — `TestLegalPublish_RefusesABodyBiggerThanTheCeiling`: tam 256 KiB
  yayımlanır, +1 bayt 413. Buradaki sınır o sayıdan BÜYÜK olmalı; 320k = sınır + 64 KiB pay
  (nginx'te k = 1024 bayt). Rota oturum kapısının arkasındadır, ama nginx gövdeyi uygulamadan
  ÖNCE tamponlar: oturumsuz bir istemci de bu yola 320 KiB'a kadar gönderebilir (uygulama 303
  ile sign-in'e yollar) — sayılı sınır, tek yol.

Her iki kaynak aynı `tls` bloğunu taşır (aynı sır, `ops-taptime-tls` — hiçbir müşteri
host'unun sırrı değil, yani takılan bir ops sınaması müşteri sertifikasını rehin alamaz);
`cert-manager.io/cluster-issuer` yalnız `tappa-operator`'da (tek Certificate). Pinler:
`TestPackaging_TheOperatorIngressesShareOneHostAndOneCertificate`,
`TestPackaging_EveryIngressHostIsRoutedByOneFile` (her host tek dosyada; joker host yok;
`TAPPA_BASE_URL`'in host'u 40'ta, 41'de değil).

**DNS — ölçüldü (2026-10-10):** `dig +short ops.taptime.mt` → `144.76.158.60` (origin;
Cloudflare kenar adresi değil). ⚠️ `deploy.yml`'in Cloudflare kapısı
(`scripts/verify-deployment.sh cloudflare`) yalnız `taptime.mt`'yi yoklar; `ops.taptime.mt`
için bir kapı **yoktur** (bugün tap işlemediği için; K4 gelirse gerekir — aşağıda).
→ **T119 (2026-10-14): kapı eklendi** (*"Preflight — the operator hostname must NOT be proxied
by Cloudflare"*), K4'süz de — gerekçe aşağıdaki *"T119 — üç kapı"*. Yeniden ölçüldü
(2026-10-14, dışarıdan, salt-okur): `dig +short ops.taptime.mt` → `144.76.158.60`; `curl -sSI -k
https://ops.taptime.mt/healthz` → `HTTP/2 404`, `cf-ray` yok.

🔴 **Bu iki Ingress yalnız yüzey YAPILANDIRILMIŞKEN zararsızdır.** Uygulamanın müşteri
yarısındaki host kapısı (`internal/httpx/operatorhost.go`, `operatorHostOnly`) operatör
host'unda her müşteri rotasını 404'e çevirir — ama `NewRouter` onu **yalnız
`TAPPA_OPERATOR_HOST` doluyken** bağlar (`internal/httpx/router.go`;
`TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide`'ın CONTROL satırları). OP-7'nin dört
anahtarı yokken `ops.taptime.mt` **müşteri ürününü** sunar (açılış sayfası, giriş formları, tap
ve aktivasyon rotaları): müşterinin **ikinci, kapısız** bir origin'i — `40-ingress.yaml`'ın
*"EXACTLY ONE HOST SERVES THE APP"* değişmezi kırılır. Bugün yumuşatan, okunarak (test
edilmeden): ürün çereze `Domain` koymaz (host'a özgü; `taptime.mt`'nin oturumu bu host'a
gitmez, buradaki bir tap §5 satır 3'tür — aktivasyon sayfası, kayıt yok) ve DNS kaydı
proxy'siz (DNS-only, yukarıdaki ölçüm).
Yani **yüzeyi kapatmak bu iki nesneyi de kaldırmak demektir**, ve deploy kimliği Ingress
**silemez** (`01-rbac.yaml`'da `delete` yok, `TestPackaging_TheDeployAppliesEveryIngressFile`
bunu da pinler): sıra yukarıdaki *"Geri alma — yüzeyi kapat"*ta. ⚠️ **Taze bir kümede** aynı
aralık kendiliğinden doğar: yüzey ancak ilk deploy'dan SONRA yapılandırılabilir (OP-7'nin
sırası: 00026 önce iner), ve o deploy 41'i de uygular — DNS o kümeye bakıyorsa, OP-7'nin 3.
adımına kadar `ops.taptime.mt` müşteri ürününü sunar. O adımı ilk deploy'un hemen ardından koş.
→ **T119 (2026-10-14): bu aralık artık doğmaz** — taze kümedeki deploy 41'i uygulamaz (yüzey
kapalı); OP-7'nin 5. adımından sonra deploy yeniden koşturulur (o bölümde).

**T119 — üç kapı (2026-10-14).** Seçenekler ölçüldü (kart: T119); uygulanan (A) + (C) + Cloudflare
kapısı, (B) uygulanmadı:

- **(A) Koşullu uygulama — ön kapı.** `deploy.yml`'in *"Preflight — the operator host is routed
  only to a server that gates it"* adımı, hiçbir şey uygulanmadan önce,
  `scripts/verify-deployment.sh operator-surface tappa` koşar. **`tappa-secrets`'ın hiçbir değeri
  okunmaz** (2. tur): `kubectl describe secret tappa-secrets` anahtar adlarını ve **bayt
  sayılarını** basar, değerleri tasarım gereği basmaz, ve yalnız `TAPPA_OPERATOR_HOST`'un bayt
  sayısı ayrıştırılır; describe'ın çıktısı doğrudan `awk`'a gider, bir kabuk değişkenine girmez
  ve basılmaz. Yetki: deploy kimliğinin **zaten sahip olduğu** `get` (`01-rbac.yaml`, tek
  `resourceName`; yeni fiil yok). Ölçüldü (k8s.io/kubectl v0.35.4'ün `SecretDescriber`'ı, kazıma
  modülde sahte bir clientset'e karşı, kümeye bağlanmadan): tek API çağrısı `get secrets` (olay
  listesi yok); çıktı `KEY:  N bytes` satırları; `last-applied` ek açıklaması atlanır; marker
  değerler çıktıda yok. (Tek istisnası, servis hesabı türündeki bir Secret'ın kendi anahtarı —
  bu Opaque Secret'ın değil. Secret bulunamazsa komut Secret'ları LİSTELEMEYE düşer;
  bu kimlikte `list` yok → hata, `UNCHECKED`.) Anahtar **>0 bayt** → `on` → *"Roll out the
  server"* 41'i rollout **beklendikten sonra** uygular (her pod anahtarla açılmış ve
  `config.Load`'dan geçmiş olur); yok ya da **0 bayt** ve 41'in iki nesnesi yok → `off`, 41
  atlanır (`::notice::`); yok/0 bayt ama nesneler duruyor → **kırmızı, rollout'tan önce** (deploy
  kimliği nesneleri silemez; komut mesajda). Okumalardan biri düşerse ya da describe'ın çıktısı
  ayrıştırılan biçimde değilse `UNCHECKED`, çıkış 2. **Pinler (4. tur: kötü yazımların listesi
  değil, BEYAZ LİSTE — kara liste iki tur üst üste aşıldı):**
  `TestVerifyDeployment_NothingReadsTheSecretsValues` — betik ve `deploy.yml` mantıksal
  satırlarla okunur (yorum atılır, `\` devamları birleştirilir); kubectl taşıyan ve saf bir
  `echo`/`printf` mesajı olmayan her satır bir çağrıdır (`sh -c '…'`, `eval`, `| sh`, tırnak
  içi `$(…)`, heredoc gövdesi dahil); secret'ı herhangi bir harf ya da tırnakla anan her çağrı,
  o dosyanın beyaz listesindeki satırlardan biri olmak **zorundadır** — betikte yalnız bu
  describe, `deploy.yml`'de yalnız Secret ön kapısının varlık kontrolü; değişkende/alias'ta
  tutulan ya da `"$@"` ile argüman aktaran kubectl reddedilir. Biçim tablosu:
  `TestVerifyDeployment_TheSecretCallScanClassifiesEveryShape`. Çalışma anı:
  `TestVerifyDeployment_EveryBranchTouchesTheSecretOnlyByDescribe` betiğin her modunu ve her
  hata dalını (404 döngüsünün sonu dahil) sahte bir kubectl'e karşı koşar; kaydedilen
  çağrılardan secret anan tek çağrı `describe secret tappa-secrets`'tır ve betiğin bastığı her
  `::error::`/`::warning::`/`::notice::` en az bir koşuda basılmış olmalıdır (sürülmeyen dal
  kırmızı). Statik olanın göremediği tek biçim kendi satırında `$x get secret …` (değişken başka
  yerde kurulmuş) — onu çalışma anı testi görür, dal koşulduysa.
  ⚠️ **Tarihli not — neden değer okunmuyor (1. tur, 2026-10-14):** ön kapı önce değeri
  `-o go-template` ile okuyup kubectl'in içinde 41'in host'uyla karşılaştırıyordu. Ölçüldü
  (k8s.io/cli-runtime v0.35.4'ün `GoTemplatePrinter`'ı, kazıma modülde): şablon bir **çalıştırma**
  hatası verirse kubectl nesnenin **tamamını — Secret'ın her değerini, `TAPPA_TAG_KEK` dahil,
  base64 — stdout'a** yazar (*"raw data was: …"*). Betik onu basmıyordu; ama depo ve Actions
  log'ları herkese açık ve tek satırlık bir düzenleme (`set -x`, bir `echo`) bütün sırları
  yayımlardı. Savunmayı sıkılaştırmak yerine sınıf kaldırıldı (orkestratör bulgusu F1).
- **(C) Rollout sonrası yoklama.** *"Gate — the operator host must not serve the customer
  product"*: `verify-deployment.sh operator-host "$OPERATOR_SURFACE" ops.taptime.mt` host'un
  `/`'sine `HEAD` atar ve cevabı **ön kapının kelimesine göre** yargılar (`OPERATOR_SURFACE` aynı
  adımın çıktısından). **`/healthz` değil, ölçüldüğü için:** bu kümenin ingress-nginx'i hiçbir
  Ingress'in yönlendirmediği bir ada `/healthz` için **200** (boş gövde) döner — kapısız
  uygulamanın verdiği durumun aynısı. `/` üçünü ayırır (ölçüldü 2026-10-14, dışarıdan,
  salt-okur): kapısız uygulama `200` (açılış sayfası; `taptime.mt`), kapılı uygulama
  `303 → /operator` (`ops.taptime.mt`), yönlendirilmeyen ad `404`. Sözlük (3. tur):

  | Kelime | geçer (pass, çıkış 0) | VIOLATION (çıkış 1) | UNCHECKED |
  |---|---|---|---|
  | `on` | **yalnız** `303 → /operator` | `2xx`, hemen | `404` 18 denemede de (5 sn arayla, ~90 sn) → çıkış 1; başka her cevap → çıkış 1; ad çözülmüyor → uyarı, çıkış 0 |
  | `off` | `404`; çözülmeyen ad | `2xx` | başka her cevap (`303` dahil) → çıkış 1 |
  | yok (`all`'da ön kapı kelime vermediyse) | — | `2xx` | çözülmeyen ad → uyarı, çıkış 0; başka her cevap → çıkış 1 |

  **Neden `on` iken 404 geçmez (3. tur, orkestratör bulgusu T119-C):** ingress-nginx yeni bir
  Ingress'i **eşzamansız** yayına alır; 41'in ilk yaratıldığı koşuda tek bir deneme büyük
  olasılıkla varsayılan sunucunun 404'ünü görürdü — yoklamanın yakalamak için var olduğu iki
  durum (başka bir host'u adlandıran anahtar, yeniden başlatılmamış pod'larla yeniden koşu) tam o
  koşuda doğar. **Neden 18 × 5 sn (gerekçe, kümede ölçülmedi):** ingress-nginx yapılandırma
  eşitlemelerini hız sınırıyla yapar (`--sync-rate-limit`, belgelenmiş varsayılan saniyede 0,3 →
  ~3,3 sn'de bir) ve her birinden sonra yeniden yükler; 5 sn bir eşitleme döneminden uzun, yani her
  deneme yeni bir yapılandırma görebilir; 90 sn ~27 dönemdir — paylaşılan, yoğun bir denetleyiciye
  yeter, işin 30 dk zaman aşımına göre kısadır. Sayılar ortamdan (`OPERATOR_HOST_PROBE_TRIES`,
  `OPERATOR_HOST_PROBE_INTERVAL`) değiştirilebilir — testler beklemeden koşsun diye; deploy ikisini
  de vermez. Yalnız **raporlar** (apply'dan sonra koşar), ama anahtarın bayt sayısının
  söyleyemediğini görür: başka bir host'u adlandıran bir `TAPPA_OPERATOR_HOST` ve aynı commit'in
  yeniden koşusu (pod yenilenmez, pod'lar açıldıkları ortamla kalır).
- **Cloudflare kapısı `ops.taptime.mt` için de.** K4 kalktığı için XFF atlatması konu değil; ama
  operatör girişi artık yalnız `internal/operatorauth`'un **adres başına** bütçeleriyle tutulur
  (flood 300, iş 20, enrollment 3 / 10 dk — `limits.go`'dan okundu, proxy arkasında ölçülmedi).
  Proxy açıkken ingress `X-Forwarded-For`'u kenarın adresiyle değiştirir ve uygulama
  (`TAPPA_TRUSTED_PROXIES` = `10.42.0.1/32`) her istemciyi bir kenar adresi olarak görür: aynı
  kenardan geçen bir yabancı bu bütçeleri harcayıp operatörü pencere boyunca kilitler (`429`, ya
  da doğru parolanın reddi). Kapı `taptime.mt`'ninkinin aynısı, kendi adımında.
- **(B) Uygulama içi — uygulanmadı.** `TAPPA_OPERATOR_HOST` boşken uygulama operatör host'unu
  bilmez (o anahtarın kendisi); host listesi (`TAPPA_BASE_URL`'in host'u dışını reddet) kubelet
  sondalarını (Host = pod IP'si), geliştirmeyi (`localhost`) ve testlerde 66 dosyadaki 128
  `NewRouter` çağrısını etkiler — bir güvenlik sınırı değişikliği ve ADR ister. (A)+(C) deploy
  yolunu kapatıyor; (B) derinlemesine savunma olarak açık iş.

**Kalan, sayılı:** (i) deploy nesneleri **silemez** — yüzey kapanırken silmek operatörün (artık
deploy bunu sessizce geçmez, kırmızı biter); (ii) ön kapı ile rollout arasında Secret'a yazılan bir
değişiklik görülmez; (iii) aynı commit'in yeniden koşusunda pod'lar eski ortamlarıyla kalır —
(C) bunu **raporlar**, önlemez (yüzey `on` iken 41'in yeni yaratıldığı koşuda da: 404'ü geçirmez,
`303`'ü ya da `2xx`'i bekler); (iv) ad çözülmüyorsa (C) yüzey `on` iken UNCHECKED geçer (uyarı,
çıkış 0), `off` iken pass — o durumda ada bir tarayıcı da ulaşamaz; (v) (B) yok: kümede elle uygulanan bir Ingress (deploy dışı) yine kapısız bir
origin açabilir — (C) onu da görür, sonradan; (vi) **değer okunmadığı için** (A), başka bir host'u
adlandıran bir `TAPPA_OPERATOR_HOST`'u `on` sayar (bir uzunluk karşılaştırması eşitlik değildir,
yarım bir kontrol bilerek eklenmedi): uygulama o host'u kapılar ve `ops.taptime.mt`'de müşteri
ürününü sunar — bunu rollout'tan sonra (C) `2xx` → VIOLATION olarak yakalar; (vii) (C)'nin
18 × 5 sn'lik beklemesi ölçülmedi: daha yavaş bir denetleyicide `on` koşusu UNCHECKED ile kırmızı
biter (yeniden koşturulur; fail-closed).

**K4 — KARAR VERİLDİ (2026-10-14): IP kısıtı YOK.** Kullanıcının sözü: *"ip limiti olmasın"*.
İki nesnede de kaynak adres listesi yoktur ve olmayacaktır; dosyanın yer tutucu yorumları artık
kararı söyler. Sonucu, sayılı: çaresi *"K4"* diye yazılmış sınırlar — **L2** (dağıtık enrollment:
dört hız anahtarı süreç geneli bütçeyi tüketir; `internal/operatorauth/limits.go`) ve **L20**
(girişin bcrypt'i için süreç geneli tavan yok; ADR 0020 §4 OP-8 notu) — artık **çaresi olmayan**
sayılı sınırlardır. Pin: bir `*source-range` ek açıklaması iki testi kırmızıya çevirir
(`TestPackaging_TheOperatorIngressesAreTheLiveObjects`,
`TestPackaging_TheOperatorIngressesShareOneHostAndOneCertificate`). Karar bir gün geri alınırsa
eski taslağın dört kuralı geçerlidir: (1) liste **iki kaynağa da** yazılır — ek açıklama kaynak
başınadır, yalnız birine yazılırsa öbür yol (ör. `/operator/legal`) her adrese açık kalır; (2) ek
açıklamanın adı kümenin ingress-nginx sürümüne göre doğrulanır (eski sürümler
`whitelist-source-range`, yeniler `allowlist-source-range` okur — ölçülmedi); (3) liste ancak
kayıt **DNS-only** kaldıkça anlamlıdır: proxy açıksa ingress istemciyi değil kenarın adresini
görür; `use-forwarded-headers` + `proxy-real-ip-cidr` (40-ingress.yaml'ın (b) seçeneği)
PAYLAŞILAN ConfigMap'tedir (~20 uygulama) ve CIDR yanlışsa origin'e doğrudan gelen biri sahte
`X-Forwarded-For` ile listeyi geçer (`cloudflare ops.taptime.mt` kapısı T119'dan beri var);
(4) liste canlıyı değiştiren bir alandır, iki pin onunla birlikte güncellenir.

**`nginx -T` — orkestratörün ölçeceği (salt-okur, ajan koşmaz).** İki kaynağın tek `server`
bloğunda birleştiğini ve her `location`'ın kendi sınırını taşıdığını gösterir:

```bash
kubectl --context hetzner-k8s-1 -n ingress-nginx get deploy,ds -o name
```

controller adı yukarıdan; aşağıda deploy/ingress-nginx-controller varsayıldı

```bash
kubectl --context hetzner-k8s-1 -n ingress-nginx exec deploy/ingress-nginx-controller -- nginx -T 2>/dev/null \
  | grep -c '## start server ops.taptime.mt'
```

beklenen: 1 (iki kaynak, TEK server bloğu)

```bash
kubectl --context hetzner-k8s-1 -n ingress-nginx exec deploy/ingress-nginx-controller -- nginx -T 2>/dev/null \
  | awk '/## start server ops.taptime.mt/,/## end server ops.taptime.mt/' \
  | grep -E 'location |set \$ingress_name|client_max_body_size|force_ssl_redirect'
```

Beklenen (sıra değişebilir): `location = /operator/legal` altında `set $ingress_name
"tappa-operator-legal";` ve `client_max_body_size 320k;`; `location /` altında `set
$ingress_name "tappa-operator";` ve `client_max_body_size 24k;`; ikisinde de
`force_ssl_redirect = true`. Bir sertifika yenilemesi sürerken ek bir
`/.well-known/acme-challenge/…` location'ı görünebilir. **Kırmızı:** iki `server` bloğu, ya da
`location = /operator/legal`'ın `24k` taşıması (o zaman 256 KiB'lık bir yasal metin
yayımlanamaz — nginx'in markasız 413'ü).

**Ingress neyi log'lar (ADR 0020 §6'nın OP-9'a bıraktığı ölçüm).** ingress-nginx'in
varsayılan erişim satırı istek satırını (`$request`: yöntem, yol, **sorgu**) ve `Referer`
başlığını yazar (ingress-nginx belgesi; kümede ölçülmedi). Ölçüldü (2026-10-02, yerel ham
TCP dinleyici, link biçiminde bir URL, sır yerine 43 karakterlik rastgele bir değer): Go
`net/http`, curl 8.7.1 ve headless Chrome 154'ün gönderdiği baytlarda — istek satırı ve
Chrome'un alt kaynak isteklerindeki dört `Referer` başlığı dahil — hesap id'si **var**,
fragment ve sır **yok**. Yani ingress satırı hesap id'sini taşır (kimlik bilgisi değil),
link sırrını bu üç istemcide taşımaz; başka istemciler ölçülmedi.

---

## Transactional e-mail (M10 EM-3) — SMTP kimliği ve iki akış

**Ne:** işlemsel e-posta (ADR 0022) **iki akışla** açılır, ikisi de
`deploy/k8s/05-config.yaml`'da: `TAPPA_RESET_DELIVERY` (`none` | `email` — panel parola
sıfırlama linki **ve** M10 EM-9'dan beri *"parolanız değişti"* bildirimi; ikisi aynı anahtarla
açılır) ve `TAPPA_INVITE_DELIVERY` (`panel` | `email` — çalışan aktivasyon linki).
Röle AWS SES'in SMTP arayüzüdür (`eu-central-1`, STARTTLS, port 587). Sır olmayan ayarlar
ConfigMap'tedir (`TAPPA_SMTP_HOST`, `TAPPA_SMTP_PORT`, `TAPPA_MAIL_FROM`,
`TAPPA_MAIL_REPLY_TO`); iki **kimlik bilgisi** `tappa-secrets`'tadır (`TAPPA_SMTP_USERNAME`,
`TAPPA_SMTP_PASSWORD`) ve `20-app.yaml` onları `optional: true` ile okur
(`TestPackaging_TheSMTPCredentialsAreOptionalSecretKeys`). Kök sertifika ayarı **yoktur**:
röle imajın sistem kökleriyle doğrulanır (Dockerfile `ca-certificates.crt`'yi kopyalar);
özel bir kök havuzu — bir CA dosyası, `SSL_CERT_FILE`/`SSL_CERT_DIR`, `/etc/ssl` ya da
`/etc/pki` altına (ya da üstüne) bağlanan bir volume, `subPath` ile eklenen tek bir PEM
dahil — **yasaktır** (ADR 0022 §2: kökü denetleyen taraf SMTP kimliğini ve her linki
okur. Go Linux'ta `/etc/ssl` ve `/etc/pki` altındaki altı demet yolundan yalnız var olan
ilkini, ve `/etc/ssl/certs` ile `/etc/pki/tls/certs` dizinlerinin **her** dosyasını okur —
go1.27.1 `crypto/x509/root_linux.go`. Bu yüzden uygulama manifestinde bugün **hiç bildirilmiş
mount yoktur** — o yollardan geniş, güvenli yönde — ve ilk mount bir pini kırmızıya çevirir).

**Bugünkü durum — 2026-10-09'dan beri iki akış da AÇIK (`email` / `email`).** Kullanıcı
kararı (EM-5B; [ADR 0022](../docs/adr/0022-islemsel-eposta.md) *"EM-5B notu"*), pin
`TestPackaging_TheConfigMapShipsTodaysDelivery`. Açılışın önkoşulları karşılandı: süreç
geneli kesici (EM-7A), alıcı başına tavan (EM-7C — sıfırlamada hesap başına **tek canlı
link**, davette işletme + posta kutusu başına saatte 5, günde 20), numaralandırma
kanallarının ve işletmeler arası davet yoğunlaşmasının **kullanıcı kabulü**, ve gerçek SES
ölçümü (aşağıda, *"EM-5B ölçümü"* — SPF/DKIM/DMARC geçti). İki kimlik `tappa-secrets`'ta
(ölçüldü). Tek taşıyıcı ve tek kesici iki akışa birden hizmet eder; sıfırlama gönderimi
istek yolunun dışında bir işçide yapılır ve kapanışta HTTP boşaltmasıyla eşzamanlı
boşaltılır (ADR 0022 §6). Sevk edilen ikilinin bu ConfigMap'in **gerçek** değerleriyle
üretimde açıldığı `TestArtifact_BootsInProductionWithTheShippedConfigMap`'te ölçülür
(kapalı DB portuyla her koşuda; `DATABASE_URL` varsa tam açılış + SIGTERM).

**Akışlar kapalıyken** (açılıştan önceki ara durum ya da aşağıdaki *"Geri alma"* sonrası)
SMTP değişkenleri **okunmaz ve doğrulanmaz**: `tappa-secrets`'ta kimlik olsa da olmasa da
süreç aynı açılır. **Akışlardan biri açıkken** okunur ve doğrulanır — eksik ya da bozuk bir
ayar yeni pod'un açılmasını durdurur (3. adımın *"Açılış reddi"*). Aşağıdaki 0–2. adımlar
kimlik **döndürme** ya da yeni bir kümede kurulum için de geçerlidir; **3. adım yapıldı
(2026-10-09)** — açılıştan geriye kalan, o adımdaki canlı duman kontrol listesidir.

### 🔴 Sıra

1. **Kullanıcının dış adımları** — [m10-platform.md](../docs/plan/m10-platform.md) §4
   *"Kullanıcının dış adımları"* 1–9 (SES kimliği `taptime.mt`, DKIM, MAIL FROM
   `mail.taptime.mt`, DNS, yapılandırma seti, production access, IAM) ve
   [ADR 0022](../docs/adr/0022-islemsel-eposta.md) §11: yapılandırma seti
   `tappa-transactional` **TLS Require**, açılma ve tıklama izleme **KAPALI** (tıklama izleme
   linki — yani davet kodunu ya da sıfırlama token'ını — AWS'nin yönlendiricisinden geçirir),
   geri bildirim yönlendirmesi kapalı ya da bildirim adresi **yalnız operatörün** okuduğu bir
   kutu, IAM yalnız `ses:SendRawEmail` (kaynak: kimlik + yapılandırma seti ARN'si, koşul
   `ses:FromAddress = no-reply@taptime.mt`). SMTP kullanıcı adı ve parolası AWS konsolunda
   **bir kez** gösterilir → doğrudan parola yöneticisine; sohbete, commit'e, ekran
   görüntüsüne, bir dosyaya **değil** (dış adım 9, Olay A-0).
2. **Bu bölümün 0–2. adımları** — kimlikler `tappa-secrets`'a (dış adım 10).
3. **Akışı aç** (3. adım) — ✅ **yapıldı, 2026-10-09** (iki akış da `email`; 1. ve 2.
   maddeler önce tamamlandı, kimlikler ölçüldü). Kural yeniden açılışta da aynıdır: ancak
   **2. maddeden SONRA**. Kimlik eksikken bir akış `email` olursa yeni pod açılmayı
   **reddeder** (`config.Load` eksik değişkeni adıyla söyler, değerini asla);
   `maxUnavailable: 0` eski pod'u servis verirken tutar, rollout zaman aşımına uğrar ve
   deploy başarısız görünür — **ama bu güvence kısa ömürlüdür**, 3. adımın *"Geri
   alma"*sı nedenini yazar.

Her `kubectl` satırı **`--context hetzner-k8s-1 -n tappa`** taşır. **Hiçbir değer ekrana,
bir dosyaya, bir komut satırına ya da bu belgeye yazılmaz** — örnek bir değer bile: değerler
`read -rs` ile yerel kabuğun değişkenlerine terminalde **yankılanmadan** girer, `printf`
(kabuğun yerleşiği — süreç argümanı olmaz; `kubectl create secret --from-literal` ise değeri
`ps`'te görünen bir argüman yapar) ile stdin'e verilir, ve sonunda `unset` edilir.

### 0) Secret'ı kim yönetiyor — ölç

```bash
kubectl --context hetzner-k8s-1 -n tappa get externalsecret
```

"No resources found" → düz Secret yolu (1a). tappa-secrets listeleniyorsa → 1b: Secret'a doğrudan
yazılan anahtarları bir sonraki eşitleme SİLER.

### 1a) İki kimliği `tappa-secrets`'a ekle — düz Secret yolu, TEK yazma

- `printf 'SES SMTP kullanici adi: '; read -rs SMTP_USER; echo`: yapıştır, Enter — ekranda görünmez

```bash
umask 077
printf 'SES SMTP kullanici adi: '; read -rs SMTP_USER; echo
printf 'SES SMTP parolasi: ';      read -rs SMTP_PASS; echo
printf '{"stringData":{"TAPPA_SMTP_USERNAME":"%s","TAPPA_SMTP_PASSWORD":"%s"}}' "$SMTP_USER" "$SMTP_PASS" \
  | kubectl --context hetzner-k8s-1 -n tappa patch secret tappa-secrets --type merge --patch-file /dev/stdin
unset SMTP_USER SMTP_PASS
```

beklenen: secret/tappa-secrets patched (kubectl'in --patch-file bayrağı gerekir). Son satır iki
değişkeni yamanın sonucu ne olursa olsun aynı yapıştırmada siler — bu yüzden ayrı bir blokta değil.

`read -r` değeri olduğu gibi alır, baştaki/sondaki boşlukları kırpar (yapıştırmanın satır
sonu dahil). SES'in SMTP parolası base64'tür (`+`, `/` taşıyabilir, `"` ya da `\`
taşımaz), yani JSON'a kaçışsız girer. **İkisi TEK yamada:** akışlar kapalıyken önemi yok,
ama bir akış açıkken yarım bir çift açılışı durdurur.

### 1b) external-secrets yolu (Secret'ı bir `ExternalSecret` yönetiyorsa)

İki değeri parola yöneticisinden **doğrudan** Infisical'a, `/tappa/` altına aynı adlarla gir
(kabuk gerekmez). Sonra canlı `ExternalSecret`'in `data:` listesine iki girdiyi **birlikte**
ekle (`deploy/examples/externalsecret.example.yaml`'da yorumda duruyorlar) ve uygula;
`kubectl --context hetzner-k8s-1 -n tappa get externalsecret tappa-secrets` →
`SecretSynced`. ⚠️ `ExternalSecret`'te anahtar başına `optional` yoktur: uzak değerlerden
biri eksikse **bütün** eşitleme durur — ikisini Infisical'a girmeden girdileri açma.

### 2) Doğrula — yalnız ADLAR ve BOYUTLAR

```bash
kubectl --context hetzner-k8s-1 -n tappa describe secret tappa-secrets | grep TAPPA_SMTP_
```

beklenen iki satır. SES'te kullanıcı adı bir erişim anahtarı kimliğidir (20 bayt), SMTP parolası 44
bayt — AWS belgesine dayanır, burada ölçülmedi. Fazlası değere bir karakter (çoğunlukla 1b yolunda
yapıştırılmış bir satır sonu) girdiğini gösterir: değeri yeniden gir (açılış bir satır sonunu
kontrol karakteri diye reddeder, bir boşluğu reddetmez).

Secret'ın değişmesi çalışan pod'u yeniden başlatmaz. Akışlar kapalıyken süreç bu iki
anahtarı zaten okumaz — **rollout gerekmez**. **Akışlar açıkken (2026-10-09'dan beri)**
çalışan süreç eski değerleri kendi ortamında tutar: yeni değer ancak pod yeniden
başlayınca girer (`rollout restart` — aşağıdaki *"Döndürme"*).

### EM-5B ölçümü — gerçek SES'e karşı (3. adımdan ÖNCE; yapıldı 2026-10-09)

**Ne:** *Transactional e-mail* bölümünün **3) Akışı aç** adımı EM-5B'nin gerçek SES ölçümlerinden **sonra**dır
([m10](../docs/plan/m10-platform.md), *"EM-5B — kalan ve neden kaldığı"*). Ölçüm iki
araçtır, ikisi de `internal/handler`'da ve **`-tags realsmtp` olmadan derlenmez bile** —
CI, `go test ./...` ve `make check` gerçek bir e-posta gönderemez
(`internal/handler/realsmtp_test.go`; araçların kendisi her koşuda test içi röleye karşı
sınanır: `internal/handler/realsmtpkit_test.go`):

- **Gönderici** (`TestEM5B_SendToTheRealRelay`) ürünün dört iletisini — sıfırlama linki,
  *"parolanız değişti"* bildirimi, VIES-doğrulanmış ve doğrulanmamış işletmenin daveti —
  **üretim zinciriyle** (`mail.New` → kesici → kanal; davet alıcı tavanı üstünden) tek bir
  adrese, birer saniye arayla gönderir; sonra **yanlış parolalı** bir taşıyıcıyla bir
  sıfırlama dener (SES'in reddi sınıf + kod olarak ölçülür, ileti gitmez). Linkler gerçek
  token/kod **değil**, sabit fikstür sözcükleridir (taban `https://taptime.mt`) ve hiçbir
  şeyi etkinleştirmez. İleti başına yalnız şunu basar: tür, gönderildi/reddedildi,
  `message_id`, `class`, `smtp_code`. **Adres, kimlik ve gövde basılmaz; dosya yazılmaz.**
- **Denetleyici** (`TestEM5B_CheckAReceivedSource`) alıcı kutusundan indirilen **bir**
  ham kaynağı (`.eml`, alıcının `Authentication-Results`'ı dahil) okur ve M7-04 + EM-5B
  kriterlerini bir tabloyla verir (kriter · beklenen · bulunan · PASS/FAIL/INFO/N/A, altında
  bir hüküm satırı ve teşhis notları): SPF pass + `smtp.mailfrom` alanı `mail.taptime.mt`;
  DKIM pass `d=taptime.mt` (SES'in kendi `amazonses.com` imzası **kaydedilir**, INFO —
  DMARC `taptime.mt` imzasına dayanır); DMARC pass; SES izlerinde bölge yalnız `eu-central-1`;
  SES → alıcı aşamasında TLS; From / Reply-To / To / sabit ASCII Subject; Date;
  `Message-ID` bizimki mi SES'inki mi (INFO — EM-5B'nin sorusu); göndericinin
  `message_id`'sinin başlıklarda geçtiği; `multipart/alternative` (text + html, UTF-8);
  her parçada **tek** URL ve link **değişmemiş**; `awstrack` / `<img` / `src=` 0;
  doğrulanmamış davette ad 0 (doğrulanmışta iki ad var — kapının pozitif kontrolü);
  kaynakta SMTP kimliği 0 (tamamı ve her 8 karakterlik parçası); link değeri yalnız
  fikstürün ve 43 karakterlik base64url (gerçek token/kod biçimi) dizisi 0.
  **Alıcı karar yazmadıysa** (kaynakta hiç `Authentication-Results` yok) SPF, DKIM
  `taptime.mt` ve DMARC satırları FAIL **değil**, PASS **hiç değil**: `N/A` olur, tablonun
  altı *"not verified by this source (N/A), NOT passed"* der, hüküm `INCOMPLETE`'tir ve
  test **kırmızı** kalır — bu üçü aşağıdaki bağımsız yolla doğrulanıp karta yazılır.
  `TAPPA_REALSMTP_SES_ID` verilmezse **250 id** satırı da `N/A`'dır (hüküm `INCOMPLETE`,
  kırmızı): kaynağı bu koşunun gönderimine bağlayan tek satır odur — onsuz, aynı
  fikstürleri taşıyan **eski** bir kaynak da PASS alırdı. Bir
  MIME sınır satırına başlık yapışmışsa (`--<sınır>Content-…`) MIME satırı bunu söyler ve
  tablonun altına *"kaynak dışa aktarımı bozuk olabilir — DKIM gövde özetine (`bh=`) bak"*
  notu düşer; denetleyici kaynağı **onarmaz** (yalnız teşhis).

**Ön koşul:** kullanıcının SES dış adımları (yukarıdaki *Sıra* 1) ve 0–2. adımlar.
Araç ConfigMap'e **dokunmaz** ve onu okumaz — kendi süreç içi zincirini kurar, kümeye
bağlanmaz; bu yüzden ConfigMap `email` iken de (2026-10-09'dan beri) aynı koşulur. 2026-10-09
ölçümü ConfigMap `none` / `panel` iken yapıldı. Hesap SES sandbox'ındaysa alıcı adres SES'te doğrulanmış olmalı.
Adres bu belgeye, bir commit'e ya da sohbete **yazılmaz**.

**Alıcı seçimi — Gmail *Download Original* önerilir** (ham kaynağı olduğu gibi verir ve
alıcının `Authentication-Results`'ını taşır; denetleyicinin iki biçimi de — `header.d`
ve Gmail'in yalnız `header.i`'si — öz-testlerde sınanıyor). **mail.tm geçici kutusu
önerilmez (ölçüldü, 2026-10-09, dört iletinin dördünde):** (1) `Authentication-Results`
**eklemiyor** → SPF/DKIM/DMARC satırları `N/A`; (2) `/sources` dışa aktarımı ilk MIME
sınırından **sonraki** CRLF'i sınırın **önüne** taşıyor (`\r\n\r\n\r\n--<sınır>Content-Transfer-Encoding:`)
→ `text/plain` parçası okunamıyor ve onu okuyan satırlar FAIL verir. Kanıt bozulmanın
SES'te değil dışa aktarımda olduğunu gösteriyor: ham kaynakta DKIM gövde özeti iki
imzada da tutmuyor, CRLF yerine konunca dört iletide iki imza da doğrulanıyor; mail.tm'nin
kendi ayrıştırıcısı da her iletide text gövdesini görüyor.

**Ortam — değer argv'ye, ekrana, geçmişe ya da bir dosyaya düşmeden.** Depo kökünde,
etkileşimli bir kabukta (bash ya da zsh). Sır olmayan dört ayar ConfigMap'teki değerlerdir;
ham kaynakların dizini de **burada**, ilk kullanımından önce tanımlanır:

- `unset TAPPA_MAIL_REPLY_TO`: ConfigMap'te boş → Reply-To beklenmez
- `EM5B_DIR=$(mktemp -d)`: 0700; ham kaynaklar (alıcı adresini taşırlar) depo dışında

```bash
export TAPPA_SMTP_HOST=email-smtp.eu-central-1.amazonaws.com TAPPA_SMTP_PORT=587
export TAPPA_MAIL_FROM='Taptime <no-reply@taptime.mt>'
unset TAPPA_MAIL_REPLY_TO
printf 'Alici adres: '; read -rs TAPPA_REALSMTP_TO; echo; export TAPPA_REALSMTP_TO
EM5B_DIR=$(mktemp -d)
```

İki kimlik **iki yoldan biriyle** girer; ikisi de testleri `em5b_env` üzerinden koşar. Bu
sarmalayıcı, test ikilisinin ortamından `tappa-secrets`'ın SMTP **dışındaki** bütün
anahtarlarını çıkarır (liste `deploy/examples/externalsecret.example.yaml`'dan; `/tappa`'ya
yeni bir sır eklenirse buraya da eklenir) — tek fren `-run` olmasın, araç onları zaten okumaz.

(A) 1a yolu — kimlikler terminalde yankılanmadan, kabuğun yerleşikleriyle (1a'daki biçim;
`read -rs -p` **kullanma**, zsh'te başka anlama gelir):

```bash
printf 'SES SMTP kullanici adi: '; read -rs TAPPA_SMTP_USERNAME; echo
printf 'SES SMTP parolasi: ';      read -rs TAPPA_SMTP_PASSWORD; echo
export TAPPA_SMTP_USERNAME TAPPA_SMTP_PASSWORD
em5b_env() { env -u DATABASE_URL -u DATABASE_MIGRATE_URL -u POSTGRES_PASSWORD -u TAPPA_APP_PASSWORD \
  -u TAPPA_TAG_KEK -u TAPPA_TAG_KEK_PREVIOUS -u TAPPA_SESSION_HMAC_KEY -u TAPPA_INVITE_HMAC_KEY \
  -u TAPPA_OPERATOR_DATABASE_URL -u TAPPA_OPERATOR_TOKEN_HMAC_KEY -u TAPPA_OPERATOR_TOTP_KEK "$@"; }
```

(B) 1b yolu — Infisical değerleri **yalnız o sürecin** ortamına verir; `/tappa`'nın bütün
sırları gelir, `env -u` SMTP dışındakileri test ikilisine varmadan düşürür:

```bash
em5b_env() { infisical run --env=<ortam> --path=/tappa -- env \
  -u DATABASE_URL -u DATABASE_MIGRATE_URL -u POSTGRES_PASSWORD -u TAPPA_APP_PASSWORD \
  -u TAPPA_TAG_KEK -u TAPPA_TAG_KEK_PREVIOUS -u TAPPA_SESSION_HMAC_KEY -u TAPPA_INVITE_HMAC_KEY \
  -u TAPPA_OPERATOR_DATABASE_URL -u TAPPA_OPERATOR_TOKEN_HMAC_KEY -u TAPPA_OPERATOR_TOTP_KEK "$@"; }
```

**EM-5B 1) Gönder** — dört ileti + bir ret, birkaç saniye:

```bash
em5b_env go test -tags realsmtp -count=1 -v -run '^TestEM5B_SendToTheRealRelay$' ./internal/handler
```

beklenen beş satır (sıra sabit):

```text
reset              sent     message_id=<SES id>
notice             sent     message_id=<SES id>
invite-verified    sent     message_id=<SES id>
invite-unverified  sent     message_id=<SES id>
auth-probe         refused  class=auth smtp_code=535
```

ve --- PASS. message_id=(empty) → 250 ya id taşımadı ya da id internal/mail'in biçim/yankı kuralına
takıldı (ADR 0022 §2, sayılı sınır 21) — test FAIL verir; satırı kartına yaz.

Dört `message_id`'yi türleriyle not et (id'ler log'a yazılabilen değerlerdir, ADR 0022
§10) — EM-5B 3. adım her birini ister. `-run` **şarttır**: onsuz `-tags realsmtp` paketin
bütün testlerini (DB testleri dahil) koşar. `-count=1` sonucu önbelleğe almaz.

**EM-5B 2) Ham kaynağı al** — her ileti için Gmail'de iletiyi aç → ⋮ → *Show original* →
*Download Original*; dosyayı `$EM5B_DIR`'e türüyle adlandırarak koy: `reset.eml`,
`notice.eml`, `invite-verified.eml`, `invite-unverified.eml`.

**EM-5B 3) Denetle** — tür başına bir kez; `TAPPA_REALSMTP_SES_ID` EM-5B 1. adımda o tür
için basılan `message_id`'dir (verilmezse o satır `N/A`, hüküm `INCOMPLETE`, test kırmızı):

```bash
export TAPPA_REALSMTP_EML="$EM5B_DIR/reset.eml" TAPPA_REALSMTP_KIND=reset TAPPA_REALSMTP_SES_ID='<reset id>'
em5b_env go test -tags realsmtp -count=1 -v -run '^TestEM5B_CheckAReceivedSource$' ./internal/handler
```

aynısı notice, invite-verified, invite-unverified için (üç değişkeni o türe çevirerek).

beklenen (Gmail kaynağı): tablonun altında "verdict: PASS", --- PASS. "message-id" satırı INFO:
"(ours: kept)" ya da "(SES's: ours replaced)" — EM-5B'nin cevabı budur, karta yazılır. "verdict:
INCOMPLETE" (N/A) ya da bir "note (mime)" → yukarıdaki "Alıcı seçimi" ve aşağıdaki EM-5B 5. adım;
test o durumda bilerek kırmızıdır.

**EM-5B 4) Kimlikleri kaldır** — tabloları karta yapıştır (adres ve kimlik tabloda yoktur),
sonra SMTP kimliklerini ve alıcıyı kabuktan çıkar. 5. adım **bundan SONRA** gelir:

- `env | cut -d= -f1 | grep -c '^TAPPA_SMTP_\(USERNAME\|PASSWORD\)$'`: beklenen: 0

```bash
unset TAPPA_SMTP_USERNAME TAPPA_SMTP_PASSWORD TAPPA_REALSMTP_TO
env | cut -d= -f1 | grep -c '^TAPPA_SMTP_\(USERNAME\|PASSWORD\)$'
```

**EM-5B 5) Bağımsız doğrulama — yalnız gerekirse** (alıcı karar yazmadıysa, ya da tabloya
ikinci göz). **Yalnız 4. adımdan SONRA ya da yeni bir kabukta** koşulur — `pip`'in indirip
kurduğu paketler bu kabuğun ortamını görür; SMTP kimlikleri dışa aktarılmışken ya da
`em5b_env`/`infisical run` altında **koşma**. Araçlar **depo dışında**, geçici bir Python
sanal ortamında (depoya bağımlılık eklenmez; değer basılmaz). Yeni kabukta `EM5B_DIR`'i
aynı dizine ayarla:

```bash
EM5B_VENV=$(mktemp -d) && python3 -m venv "$EM5B_VENV" && "$EM5B_VENV/bin/pip" install -q dkimpy pyspf dnspython
```

DKIM — kaynaktaki HER imza (d=taptime.mt ve d=amazonses.com); anahtar DNS'ten okunur

```bash
"$EM5B_VENV/bin/python" - "$EM5B_DIR/reset.eml" <<'PY'
import sys, dkim
raw = open(sys.argv[1], 'rb').read()
n = sum(1 for l in raw.split(b'\n') if l.lower().startswith(b'dkim-signature:'))
for i in range(n):
    d = dkim.DKIM(raw)
    print(i, d.verify(idx=i), d.domain)
PY
```

beklenen: her imza True, alanlar taptime.mt ve amazonses.com. mail.tm kaynağında önce ilk sınırdan
sonraki CRLF'i yerine koy (yalnız teşhis; denetleyici bunu yapmaz):

```text
raw.replace(b'\r\n\r\n\r\n--' + <sınır> + b'Content-', b'\r\n\r\n--' + <sınır> + b'\r\nContent-', 1)
```

SPF — \<SES IP>: SES'ten gelen Received satırındaki [köşeli parantez]; \<Return-Path>:
...@mail.taptime.mt; \<HELO>: aynı satırın "from" host'u

```bash
"$EM5B_VENV/bin/python" -c 'import spf,sys; print(spf.check2(i=sys.argv[1], s=sys.argv[2], h=sys.argv[3]))' \
  '<SES IP>' '<Return-Path>' '<HELO>'
```

beklenen: ('pass', ...). Kayıtlar: dig +short TXT mail.taptime.mt → "v=spf1 include:amazonses.com ~all"

DMARC: dig +short TXT _dmarc.taptime.mt; hizalama elle — From alanı taptime.mt, geçen DKIM imzasının
d= alanı taptime.mt (relaxed) → hizalı.

```bash
rm -rf "$EM5B_VENV"; unset EM5B_VENV
```

(Orkestratör 2026-10-09 ölçümünü bu araçlarla — dkimpy + pyspf, depo dışı venv — yaptı;
bu blok o yolun tarifidir ve yazıldığı ortamda ağ olmadan **koşulmadı**: ilk kullanımda
çıktısını sonuç tablosuyla karşılaştır.)

**EM-5B 6) Kapat:**

```bash
rm -rf "$EM5B_DIR"
unset EM5B_DIR TAPPA_REALSMTP_EML TAPPA_REALSMTP_KIND TAPPA_REALSMTP_SES_ID
unset -f em5b_env
```

**Neyi ölçmez:** çıkış kutusu işçisini ve istek yollarını (veritabanı ister — test içi
röleye karşı ölçülüdürler), panelin cümlelerini ve sıfırlama linkinin tek kullanımlığını
(gerçek bir link ister: **3) Akışı aç** adımından sonraki canlı duman), alıcı istemcinin linkleri nasıl
gösterdiğini. Gönderici süreç içinde **taze** bir kesici ve alıcı tavanı kurar; canlı
sürecin sayaçlarına dokunmaz (dört ileti, her iki tavanın çok altında).

**Ölçüldü — 2026-10-09, orkestratör, gerçek SES (`eu-central-1`), alıcı mail.tm geçici
kutusu** (adres ve kimlik burada yok). Gönderici: exit 0 — dört ileti `sent`, dördünde SES
250 `message_id` dolu (biçim ve yankı kuralından geçti); `auth-probe refused class=auth
smtp_code=535`. Denetleyici (2. turdaki hâliyle) dört ham kaynağı **iki hâlde** okudu:
mail.tm'nin ham dışa aktarımı ve **CRLF'i onarılmış** kaynak. Onarım **yalnız** dışa aktarımın
kaydırdığı **tek CRLF'i** ilk sınırın arkasına geri koymaktır (`\r\n\r\n\r\n--<b>Content-…` →
`\r\n\r\n--<b>\r\nContent-…`) ve iki DKIM imzası (`d=taptime.mt`, `d=amazonses.com`) **ancak bu
hâlde** doğrulanır — ham hâlde gövde özeti ikisinde de tutmaz. Yani onarılmış kaynak, SES'in
gönderip imzaladığı baytın kendisidir; onarım içerik uydurmaz, imza bunu kanıtlar.

| Satır | Ham mail.tm dışa aktarımı | CRLF onarılmış (DKIM'in doğruladığı bayt) |
|---|---|---|
| hüküm | dördünde **FAIL** | dördünde **INCOMPLETE** — hiçbir satır FAIL değil; yalnız SPF/DKIM/DMARC N/A (test bilerek kırmızı) |
| `mime` | FAIL (4/4) — `text/plain` okunamıyor | PASS (4/4) |
| `link (text part)` | FAIL (4/4) | PASS (4/4) |
| `link (html part)` · `tracking` | PASS (4/4) | PASS (4/4) |
| `names` | sıfırlama, bildirim PASS; iki davet FAIL (beklenen sözcükler okunamayan text parçasında) | PASS (4/4) — doğrulanmamışta ad 0, doğrulanmışta 2 |
| `link value` | bildirim PASS (linkinde değer yok); öteki üçü FAIL (iki yerine bir değer) | PASS (4/4) |
| `spf` · `dkim taptime.mt` · `dmarc` | N/A — mail.tm `Authentication-Results` eklemiyor (4/4 kaynakta 0) | N/A (aynı sebep) |
| bağımsız (dkimpy + pyspf, depo dışı) | — (ham hâlde DKIM gövde özeti tutmuyor) | DKIM `d=taptime.mt` ve `d=amazonses.com` 4/4 geçerli; SPF `mail.taptime.mt` (`v=spf1 include:amazonses.com ~all`, SES IP'si) 4/4 pass; DMARC hizalı (From `taptime.mt`, DKIM `d=taptime.mt`, relaxed) |
| `message-id` | INFO — **SES'inki**: bizim `<rastgele@taptime.mt>` başlığımız değiştirildi | aynı |

Ham hâldeki FAIL'lerin hepsi tek sebepten: dışa aktarımın kaydırdığı CRLF yüzünden `text/plain`
parçası okunamıyor (MIME satırının notu bunu söyler).

**Gmail "Show original" yerine bağımsız doğrulama — orkestratör kabulü, 2026-10-09.** m10'un
EM-5B kartı harfiyen *"Gmail Show original SPF=PASS (`mail.taptime.mt`), DKIM=PASS,
DMARC=PASS"* istiyor. Bu koşuda alıcı mail.tm'ydi ve karar yazmadı; üç sonuç alıcının değil,
dkimpy/pyspf'in DKIM'in doğruladığı bayt üstündeki ölçümüdür — m10 maddesi bununla **ikame
edildi**, alıcının kendi kararı ise aşağıdaki canlı dumana kaldı.

**Kalan — ConfigMap `email`'e çevrildikten sonraki canlı duman** (ConfigMap 2026-10-09'da
çevrildi; madde madde kontrol listesi **3) Akışı aç** adımında): sıfırlama linkinin tek
kullanımlığı ve M7-04 B5 (gerçek link ve istek yolu ister), B2'nin gözle teyidi (başarısız
denemenin bir yere düştüğü), ve **Gmail'de alıcı kararı** (*Show original*'da SPF, DKIM ve
DMARC'ı alıcının kendisinin PASS yazması).

### 3) Akışı aç — ✅ yapıldı 2026-10-09 (iki akış da `email`)

`05-config.yaml`'da `TAPPA_RESET_DELIVERY: "email"` (EM-5 + dış adımlar 1–10 ve geri
bildirim adımı bitince — ADR 0022 §12; bu, parola değişikliği bildirimini de açar — her
değişiklik, kayıtlı adrese bir e-posta, ADR 0022 *"EM-9 notu"*) ve
`TAPPA_INVITE_DELIVERY: "email"` (EM-7B sevk edildi; EM-7C'nin işletme + kutu tavanıyla).
**İkisi de 2026-10-09'da çevrildi** — kullanıcı kararı, EM-5B'nin önkoşulları karşılandıktan
sonra (ADR 0022 *"EM-5B notu"*: kesici EM-7A `b935f47`, davet kanalı EM-7B `a8d38ff`, alıcı
başına tavan EM-7C `d69087b`, EM-5B ölçüm aracı `4b201f8`). Bu bir **deploy kararıdır**
(`main`'e birleştirme — CLAUDE.md §10) ve onu yapan değişiklik
`TestPackaging_TheConfigMapShipsTodaysDelivery`'yi **bilerek** güncelledi (artık `email` /
`email`'i pinler); geri dönüş de aynı testi bilerek günceller. Önce 0–2. adımlar.

**Canlıya nasıl çıkar — ConfigMap değişikliği tek başına pod'u yeniden başlatmaz (ölçüldü,
`deploy.yml` + `20-app.yaml`).** Pod şablonunda ConfigMap'in özetini taşıyan bir
annotation yoktur ve `deploy.yml` `rollout restart` çalıştırmaz. Ortam konteyner başlarken
okunur (`envFrom: tappa-config`). Bu birleştirmede pod yine de yenilenir, çünkü `deploy.yml`
önce `05-config.yaml`'ı uygular, sonra `20-app.yaml`'ı **commit'in kendi imaj etiketiyle**
(`:sha-<12 hane>`) uygular — yeni commit şablonu değiştirir, Deployment yeni bir ReplicaSet
açar ve yeni pod `email`'i okur. **Rollout olmayan durum:** aynı SHA'nın yeniden deploy'u
(`workflow_dispatch` ile *Run workflow*) ya da ConfigMap'in elle `kubectl apply`'ı —
şablon değişmez, çalışan pod eski ortamda kalır; o zaman
`kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa` gerekir.
Deploy sonrası teyit (imaj `scratch`'tır — pod içinde `printenv` yok, teyit dışarıdan):

```bash
kubectl --context hetzner-k8s-1 -n tappa get configmap tappa-config \
  -o jsonpath='{.data.TAPPA_RESET_DELIVERY} {.data.TAPPA_INVITE_DELIVERY}{"\n"}'
```

beklenen: email email

```bash
kubectl --context hetzner-k8s-1 -n tappa get pods -l app.kubernetes.io/name=tappa,app.kubernetes.io/component=server \
  -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.creationTimestamp} {.spec.containers[0].image}{"\n"}{end}'
```

beklenen: TEK pod; imaj etiketi deploy edilen commit, oluşturma zamanı deploy koşusundan sonra

Pod'un **gerçekten** `email` okuduğunun ürün içi kanıtı kontrol listesinin 1. maddesidir
(kurtarma formu).

**Canlı duman — kontrol listesi (kullanıcı, kendi hesabıyla; 2026-10-09 deploy'undan
sonra).** Adres, link ve kod bu belgeye, bir commit'e ya da sohbete **yazılmaz**; sonuçlar
yalnız ✓/✗ olarak karta/`state.md`'ye geçer.

- [ ] **1. Açılış.** `rollout status` başarılı; yeni pod'un log'unda `"msg":"fatal"` yok;
  `https://taptime.mt/admin/reset` formu *"This Taptime cannot send email yet"* uyarısını
  **göstermiyor** (göstermesi = pod hâlâ `none` okuyor → yukarıdaki `rollout restart`).
- [ ] **2. Sıfırlama — kendi hesabınla *"parolamı unuttum"*.** Formda kendi yönetici
  adresin → *"Check your email"* sayfası → Gmail'de kurtarma e-postası geldi (gelen kutusu
  mu spam mi — not et) → linkle yeni parola → *"recovered"* cümlesiyle giriş formu → yeni
  parolayla giriş. Ardından **ikinci bir e-posta**: *"your password was changed"* bildirimi
  (EM-9) aynı kutuya geldi.
- [ ] **3. Tek kullanımlık link.** 2. maddedeki **aynı** linki yeniden aç ve parola koymayı
  dene → link kullanılamaz sayfası (*"That link no longer works"* ailesi); parola
  **değişmez** (yeni parolayla giriş hâlâ çalışır).
- [ ] **4. B2 — başarısız deneme bir yere düşer.** (a) 3. maddedeki deneme **çözülen**
  bir linktir → `audit_log`'da `admin.recovery.refused` satırı, `already_used: true`
  (2. maddenin satırları da aynı sorguda görünür):

  ```bash
  kubectl --context hetzner-k8s-1 -n tappa exec statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -U tappa_owner -d tappa -Atc \
    "SELECT at, action, detail->>'"'"'outcome'"'"', detail->>'"'"'already_used'"'"' FROM audit_log WHERE action LIKE '"'"'admin.recovery.%'"'"' OR action LIKE '"'"'admin.password_notice.%'"'"' ORDER BY at DESC LIMIT 6;"'
  ```

  beklenen, yeniden eskiye (2. ve 3. maddeden sonra):

  ```text
  ...|admin.recovery.refused|refused|true
  ...|admin.password_notice.sent|sent|
  ...|admin.recovery.completed|ok|
  ...|admin.recovery.requested|ok|
  ```

  requested yerine undelivered satiri varsa e-posta roleye ulasmadi: detail reason verir, sinif ve
  SMTP kodu pod logundaki "panel recovery: delivery failed" satirindadir.

  (b) linkin `t=` değerinden bir karakter değiştirip aç → aynı ret sayfası; satır
  **yazılmaz** (tenant yok, ADR 0015), yerine süreç log'unda
  `"panel recovery refused"` + `"link did not resolve"` satırı (SigNoz ya da
  `kubectl --context hetzner-k8s-1 -n tappa logs deployment/tappa -c tappa --tail=50`).
  İkisinde de token ya da özeti **görünmez**.
- [ ] **5. B5 — kayıtlı/kayıtsız ayırt edilemez.** Formu bir de **kayıtlı olmayan** bir
  adresle gönder → **aynı** *"Check your email"* sayfası, aynı cümleler, gözle fark
  edilir bir gecikme farkı yok; o adrese **hiçbir** e-posta gitmez. (Zamanlama eşitliği
  [taban, taban + 50 ms] test içi ölçülüdür; burada yalnız sayfanın aynılığı ve gönderimin
  yokluğu teyit edilir.)
- [ ] **6. Test daveti.** Panelde, e-posta adresi **senin** bir kutun olan bir test çalışanı
  için davet → ekran *"Their activation link is on its way by email"* der, link **ekranda
  gösterilmez** (`panel` modunda gösterilirdi); Gmail'de davet geldi; `audit_log`'daki son
  davet satırı `invite.code_emailed` (`invite.code_shown_to_manager` **değil**). Linki
  açmak isteğe bağlı — aktivasyon bir NFC dokunuşunda tamamlanır (ADR 0026); test
  çalışanını iş bitince panelden devre dışı bırak.
- [ ] **7. Gmail'de alıcı kararı.** 2. ve 6. maddelerdeki üç iletinin her birinde ⋮ →
  *Show original* → üst özet: **SPF PASS** (`mail.taptime.mt`), **DKIM PASS**
  (`taptime.mt`), **DMARC PASS**. `Message-ID`'nin SES'inki olması beklenen
  (EM-5B kaydı). Şüphede ham kaynak *"EM-5B 3) Denetle"* adımıyla aynı denetleyiciden
  geçirilebilir (`TAPPA_REALSMTP_SES_ID` olmadan o satır `N/A` olur).

Bir madde ✗ ise: 1 → `rollout restart` / pod log'u; 2 ya da 6 gelmediyse → `audit_log`'da
`admin.recovery.undelivered`, `admin.password_notice.undelivered`, `invite.undelivered`
ya da `invite.email_refused` satırı (davette ve bildirimde sınıf ve SMTP kodu `detail`'de,
sıfırlamada pod log'unun *"panel recovery: delivery failed"* satırında; hiçbirinde adres
yoktur), SES konsolunda gönderim ve bastırma listesi, spam klasörü; 3, 4, 5 ya da 7 ✗ ise akışları
kapatmak (aşağıdaki *"Geri alma"*) bir **deploy kararıdır** — önce orkestratöre/karta yaz.

**Açılış reddi** (rollout `status` zaman aşımına uğrar, eski pod servis verir): yeni pod'un
log'unda `"msg":"fatal"` satırı nedeni adıyla söyler — *"TAPPA_SMTP_… is required while
TAPPA_…_DELIVERY is email"* (eksik ya da yalnız boşluk), *"TAPPA_SMTP_PORT: … 465 is
implicit TLS"* (465 her ortamda red; 587 kullan), *"TAPPA_SMTP_HOST: in production the relay
must not be localhost, a \*.localhost name or an IP address"* (yalnız bu üçü — başka
loopback adları reddedilmez; sayılı sınır 1),
*"TAPPA_SMTP_HOST: must be a lower-case DNS host name"* (şema, port, büyük harf, sondaki
nokta), *"TAPPA_MAIL_FROM: refused by the transport's own rule"* / aynısı
`TAPPA_MAIL_REPLY_TO` ve iki kimlik için (`internal/mail`'in kuralı: kontrol karakteri,
`=?`, ikinci adres; Reply-To'da görünen ad), ya da *"… is valid configuration, but this
build …"* (kanalı henüz olmayan bir akış). Satırlar değişkenin adını söyler; testin
aradığı biçimlerde değeri **tekrar etmez** — iki kimlikte hiçbir 3 baytlık parçasını, öteki
değerlerde ne tamamını ne ilk 8, 4 ya da 3 karakterini
(`TestLoad_MailErrorsNameTheVariableNeverTheValue`).

**Geri alma — HEMEN, isteğe bağlı değil.** `maxUnavailable: 0`'ın *"eski pod servis
vermeye devam eder"* güvencesi yalnız eski pod **yaşadığı sürece** geçerlidir:

- deploy ConfigMap'i **uygulamanın kendisinden önce** uygular (`deploy.yml`:
  `kubectl apply -f deploy/k8s/05-config.yaml` önce) — yani canlı ConfigMap artık
  `email` der;
- eski pod **yeniden yaratılırsa** (node drain, tahliye; büyük olasılıkla konteynerin
  yeniden başlaması da — ortam konteyner başlarken okunur, ölçülmedi) **canlı**
  ConfigMap'ten okur ve o da açılmaz;
- `replicas: 1`: o an **tap yüzeyi kapanır** — hiçbir pod servis vermez.

Bu yüzden açılış reddini görür görmez: ConfigMap'te akışı `none` / `panel`'e **geri al ve
uygula** (deploy'la ya da `kubectl --context hetzner-k8s-1 -n tappa apply -f
deploy/k8s/05-config.yaml` ile), sonra başarısız rollout'u yeniden başlat.
`kubectl rollout undo` **yetmez**: Deployment'ı geri alır, ConfigMap'i **getirmez** — geri
alınan pod da `email` okur.

**Başarılı bir açılıştan geri dönüş** (e-postayı kapatmak) için de aynı ConfigMap
değişikliği gerekir, ama ConfigMap'i elle uygulamak **çalışan pod'u yeniden başlatmaz**
(yukarıda *"Rollout olmayan durum"*): pod `email`'i okumaya ve göndermeye devam eder. Bu
yüzden ConfigMap'i uyguladıktan sonra:

```bash
kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa
kubectl --context hetzner-k8s-1 -n tappa rollout status deployment/tappa --timeout=300s
```

sonra: https://taptime.mt/admin/reset formu yeniden This Taptime cannot send email yet uyarisini
gostermeli

(Değişiklik deploy'la — `main`'e birleştirme — gelirse yeni commit'in imaj etiketi
şablonu zaten değiştirir; restart'ı yalnız elle `apply`'da unutma.) **Eski bir imaja
dönülecekse** sıra ayrıdır: önce ConfigMap, sonra imaj (*"Elle deploy / rollback"*, e-posta
uyarısı). Kimlikleri de kaldırmak istersen (akışlar kapalıyken güvenli):

```bash
kubectl --context hetzner-k8s-1 -n tappa patch secret tappa-secrets --type json -p '[
  {"op":"remove","path":"/data/TAPPA_SMTP_USERNAME"},
  {"op":"remove","path":"/data/TAPPA_SMTP_PASSWORD"}]'
```

(1b yolunda önce `ExternalSecret`'in `data:` listesinden iki girdiyi kaldır — Operator
surface bölümünün *"Geri alma"*sındaki sıra.)

### Döndürme (dış adım 8; ADR 0022 sayılı sınır 10)

SMTP kimliği uzun ömürlü bir IAM anahtarıdır ve döndürmesi elledir: IAM'de **yeni** SMTP
kimliği üret → 1a (ya da 1b) yeni değerlerle → `kubectl --context hetzner-k8s-1 -n tappa
rollout restart deployment/tappa` (çalışan süreç eski değerleri kendi ortamında tutar) →
bir gönderimin geçtiğini gör → eski erişim anahtarını IAM'de **devre dışı bırak**, sonra sil.

### Geliştirme (ADR 0022 §12, EM-K9)

Geliştirmede e-posta **gönderilmez**: yerel posta yakalayıcı (Mailpit) yoktur, `.env`'de iki
akış boştur (= `none` / `panel`). Taşıyıcı `internal/mail`'in testlerindeki öz-imzalı sahte
SMTP sunucusuyla doğrulanır. `email` modu geliştirmede yapılandırma tarafından
**reddedilmez** (kabul edilen risk, ADR 0022 sayılı sınır 25; `localhost` ve IP de orada
izinlidir, 465 değil) — açılırsa:

- hedef **gerçek bir röledir** (özel kök havuzu test dışında yasak, yani yerel bir sahte
  sunucuya güvenilmez);
- 🔴 **prod SMTP kimliği `.env`'e yazılmaz:** `.env` git'te yok sayılır ve R7d'nin taradığı
  dosya listesine girmez — oraya yazılan bir değeri hiçbir tarama görmez;
- seed'deki 37 adres (`@kebabfactory.mt`, `@kebabmfg.mt`) gerçek bir davet alabilir.

**Öneri (dış adım):** yerelden SES denemesi için **ayrı**, sandbox'ta kalan, kısıtlı bir IAM
kimliği (yalnız `ses:SendRawEmail`; sandbox yalnız doğrulanmış alıcılara gönderir — SES
belgesi, ölçülmedi — bu da seed adreslerini korur). Bu derleme **iki akışta da** `email`
ile açılır (sıfırlama EM-5'ten, davet EM-7B'den beri) — dev'de `TAPPA_RESET_DELIVERY=email`
ya da `TAPPA_INVITE_DELIVERY=email` gerçek röleye gider (ADR 0022 sınır 25). Üretim
ConfigMap'inin 2026-10-09'dan beri `email` olması `.env`'i değiştirmez: orada iki akış
boş kalır.

### Sayılı sınırlar (EM-3)

1. **Röle için izinli ad listesi yok** (ADR 0022 sınır 24): ConfigMap'i değiştirebilen biri
   `TAPPA_SMTP_HOST`'u herkese açık güvenilir sertifikası olan kendi sunucusuna çevirebilir;
   TLS doğrulaması geçer, kimlik ve linkler o sunucuya gider. Üretim kuralı yalnız
   `localhost`, `*.localhost` adını ve IP adresini (her yazımıyla) reddeder; kural
   yazıma bakar, çözümlemeye değil — `ip6-localhost`, `localhost.localdomain` ya da tek
   etiketli bir ad (`relay`) üretimde **kabul edilir**.
2. **Kök deposu.** Tehdit modeli (ADR 0022 EM-3 notu, birebir): "Bu pinler manifestlere ve Dockerfile'a kazara giren sapmaya karşıdır; pini atlatmak için bilerek yazılmış bir manifest ya da imaj kod incelemesinin konusudur."
   **Pinli, fail-closed — tablolarındaki biçimlerle:** üst düzey manifestlerde
   (`deploy/k8s/*.yaml`) ve Dockerfile'da `SSL_CERT` kelimesi (yorumlar dahil; kaçış, satır
   devamı, büyük/küçük harf, kelimenin **tamamını** tutan bir `ARG`) —
   `TestPackaging_NothingMovesTheSystemRoots`; manifestlerde ters bölü ve `!!` etiketi yok;
   uygulama manifestinde **hiç bildirilmiş mount yok** — `TestPackaging_NoMountShadowsTheSystemRoots`.
   **Pinsiz** (ADR 0022 sınır 23 ve EM-3 notu; kod ve manifest incelemesinin konusu):
   - **Dockerfile'a eklenen bir `COPY … /etc/ssl/certs/` satırı** — kurumsal bir CA'yı imaja
     koymak; gerçekçi bir kazara sapma ve pinden **geçer** (R11). Kök deposu başka yoldan
     değiştirilmiş bir imaj da.
   - Masum parçalardan kurulan bir ad (`ARG P=SSL_` + `ENV ${P}CERT_FILE`, E01) ve `!!`
     taşımayan açık bir YAML etiketi (`!<tag:yaml.org,2002:binary>`, E02).
   - Taranan dosyaların dışındaki kanallar: kustomize patch'i (R06b), alt dizindeki
     kustomization (R06c), Helm `extraEnv` gibi bir değer şablonu (R07b), `deploy.yml`'e
     eklenecek bir `kubectl set env`, canlı `tappa-config`'e elle eklenmiş bir anahtar
     (`kubectl apply`'ın üçlü birleştirmesi onu silmeyebilir — ölçülmedi).
   - Pinleri atlatmak için **bilerek** yazılmış her manifest ya da imaj (tehdit modeli).

   **Bedeli:** meşru bir mount; manifestte meşru bir ters bölü ya da etiket; **hizmet dışı**
   bir manifestte (migrate Job'ı, Postgres) ya da Dockerfile'ın **build** aşamasında meşru bir
   `SSL_CERT` (R14, R15) — her biri ilgili pini kırmızıya çevirir ve o değişiklik pini günceller.
3. **Yalnız 465 reddedilir.** SES'in ikinci örtük TLS portu 2465 (SES belgesi, ölçülmedi)
   kabul edilir; o portta her gönderim süre dolana dek bekler ve `timeout` olur — kimlik
   gitmez (STARTTLS olmadan AUTH yok), ama gönderim de olmaz.
4. **Akışlar kapalıyken ayarlar okunmaz** — ConfigMap'teki bozuk bir değer açılışta değil,
   CI'da görünür (`TestPackaging_TheConfigMapsMailSettingsLoadInProduction`), Secret'taki
   bozuk bir kimlik ise ancak akış açıldığında. *(2026-10-09'dan beri akışlar açık: ayarlar
   ve iki kimlik her açılışta okunup doğrulanır; bu sınır yalnız akışlar yeniden
   kapatılırsa geçerlidir. Kimliğin **değeri** — SES'in kabul edip etmediği — açılışta değil
   ilk gönderimde görünür: `auth` sınıfı, SMTP 535.)*

---

## OPS-2 — Harbor, Tailscale OAuth ve `production` ortamı

OPS-2 (2026-10-19) deploy'un üç dış kimliğini değiştirdi: imajlar public Docker Hub'dan private
Harbor'a (`registry.everva.com.tr/tappa`, operatör adımı 4), tailnet'e katılım auth key'den OAuth
client'a (adım 5 (a)), ve deploy'un bütün secret'ları depo düzeyinden yalnız `main`'e açık
`production` ortamına (adım 5 (c)). Altyapıyı ana oturum kurdu (Harbor projesi ve iki robot, kümede
`secret/harbor-pull`, ortam ve beş secret'ı); bu bölüm ilk deploy'un **kapanış ölçüsünü** ve
**OPS-2 öncesine geri dönüşü** taşır.

🔴 **İlk OPS-2 deploy'undan ÖNCE:** `deploy/k8s/01-rbac.yaml` cluster-admin kimliğiyle **yeniden
uygulanmalı** (adım 3) — deploy kimliği `harbor-pull`'u ancak o zaman okuyabilir; yoksa *"Preflight —
the registry pull Secret…"* `Forbidden` ile, kümeye bir şey uygulanmadan, kırmızı durur.

```bash
kubectl --context hetzner-k8s-1 apply -f deploy/k8s/01-rbac.yaml
kubectl --context hetzner-k8s-1 -n tappa auth can-i get secret/harbor-pull --as=system:serviceaccount:tappa:github-deployer
```

İkinci satır `yes` demeli. (`auth can-i` alt kaynakları görmez — sınır 17 — ama burada sorulan bir alt
kaynak değil, adlı bir nesne.)

🔴 **Yine ilk OPS-2 deploy'undan ÖNCE:** depo düzeyindeki dört eski secret (`TS_AUTHKEY`,
`KUBE_CONFIG`, `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`) silinmiş olmalı — adım 5 (c)'nin bloğu; son
satırı (`gh secret list --repo atknatk/tappa`) depo düzeyinde hiç deploy secret'ı göstermemeli.
✅ **İkisi de yapıldı (2026-10-19, ilk OPS-2 deploy'undan önce):** `01-rbac.yaml` uygulandı; depo
düzeyinden **altı** secret silindi — dördü ve 2026-10-18'de depo düzeyine de konmuş
`TS_OAUTH_CLIENT_ID`/`TS_OAUTH_SECRET` kopyaları (aşağıda *"Ölçüldü"*).

### OPS-2 kapanış ölçüsü

`main`'deki ilk OPS-2 deploy koşusunun log'unda, Harbor'da, kümede ve Tailscale konsolunda — hepsi
**ölçülür**, hiçbiri varsayılmaz:

1. *"Preflight — the production environment's secrets must exist"* yeşil ve son satırı beş adı
   `present` diye sayar (değer yok, uzunluk yok).
2. *"Build both images"* yeşil: taban imaj Harbor'ın proxy projesinden, digest'iyle çekildi — derleme
   log'unda `registry.everva.com.tr/dockerhub/library/golang:1.26.9-bookworm@sha256:d9c68c2c…453c`.
   ⚠️ **Harbor'da o digest'i ARAMA:** **Projects → `dockerhub` →** `library/golang`'da görünen
   (tek, etiketsiz) artifact, sabitlenmiş index'in **linux/amd64 çocuk manifest'idir** (ilk koşuda
   `sha256:bcef992b77b1…9eff`); index'in Harbor'da hiç saklanmadığı **ölçülmedi**, yalnız listede
   görünmediği.
   Doğrulama: o digest, sabitlenmiş index'in linux/amd64 girdisi mi — `docker buildx imagetools
   inspect golang:1.26.9-bookworm@sha256:<index digest>` listesinde `Platform: linux/amd64`'ün
   `Name:` satırı, ya da Docker Hub'ın kimliksiz token'ıyla (aşağıdaki *"OPS-2 geri dönüş"*
   önkoşulunun kalıbı, `scope=repository:library/golang:pull`) `manifests/<index digest>` okuması
   (`Accept:` index türleri) ve `manifests[]`'te `platform.os == linux`, `architecture == amd64`
   girdisinin `digest`'i. Eşleşmezse o artifact pinlenen index'ten gelmemiştir — incele.
   *"Gate — both images must be pullable WITH the CI credential and NOT without one"* her imaj için
   iki satır basar: `pull WITH the registry credential … ok: registry.everva.com.tr/tappa/…` ve
   `pull WITHOUT a credential … refused: … (HTTP 401 UNAUTHORIZED)`. Kimliksiz yarı OPS-2 düzeltme
   turu 1'den beri docker'ın sözlerini değil registry'nin **HTTP kodunu ve hata kodunu** okur (curl,
   token akışı; operatör adımı 4 (e)); Harbor'ın cevabı denetçinin ölçtüğü biçimdir (`/v2/` → `401`,
   manifest → `401 UNAUTHORIZED`). Kapı başka bir cevapla kırmızı olursa cevap ölçülür ve
   `scripts/verify-image.sh --access` — testiyle birlikte — ona göre düzeltilir, kapı gevşetilmez.
3. *"Join the tailnet"* yeşil; kapı satırı `tailnet: tailscale 1.94.2, backend Running, tags tag:ci,
   ephemeral absent` (1.94.2 ephemeral'ı bildirmez → beklenen uyarı) ve `cluster API reachable at
   10.43.0.1:443 (attempt 1)`.
4. *"Preflight — the registry pull Secret…"*: `pull-secret: secret/harbor-pull in tappa is
   kubernetes.io/dockerconfigjson with a credential for registry.everva.com.tr`.
5. Koşu yeşil biter. İmajlar Harbor'da: **Projects → `tappa` →** `tappa/tappa` ve
   `tappa/tappa-migrate` depolarında koşunun `sha-<12hex>` etiketi.
6. Pod'lar Harbor'dan, `harbor-pull`'la çekti — imaj adresi, Secret adı ve çekilen imajın kimliği
   (`imageID`, `registry.everva.com.tr/tappa/tappa@sha256:…` olmalı); ikinci komut hiçbir
   `ImagePullBackOff`/`ErrImagePull` basmamalı:

   ```bash
   kubectl --context hetzner-k8s-1 -n tappa get pod -l app.kubernetes.io/component=server -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.spec.containers[0].image}{"  ps="}{.spec.imagePullSecrets[*].name}{"  "}{.status.containerStatuses[0].imageID}{"\n"}{end}'
   kubectl --context hetzner-k8s-1 -n tappa get pod -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.status.initContainerStatuses[*].state.waiting.reason}{.status.containerStatuses[*].state.waiting.reason}{"\n"}{end}'
   ```

7. Koşudan sonra Tailscale konsolunun **Machines** listesinde `github-*` cihazı **kalmaz** (OAuth
   yolunun anahtarı ephemeral; T134'ün auth key'li koşusu bir cihaz bırakmıştı). Kalırsa: elle sil ve
   `TS_OAUTH_SECRET`'ın gerçekten bir OAuth client secret'ı olduğunu doğrula.
8. Log'da e-posta adresi, status JSON'u ya da secret değeri **yok** (koşu log'unda `@` ve `tskey`
   araması; runner her `secrets.*` değerini maskeler, arama maskelenmemiş bir kopyayı arar).
9. Docker Hub token'ını ve eski Tailscale auth key'ini kendi konsollarında iptal et (adım 5 (c) bunu
   silmeden önce ister; yapılmadıysa şimdi); backlog T135'i güncelle. Depo düzeyindeki eski
   secret'ların silinmesi bu listenin maddesi **değil**, yukarıdaki ön koşuldur — dört eski deploy
   secret'ı **ve** 2026-10-18'de depo düzeyine de konmuş `TS_OAUTH_CLIENT_ID`/`TS_OAUTH_SECRET`
   kopyaları, yani **altı**.

**Ölçüldü (2026-10-19, koşu 39323391063, `34648e3`, 31 adımın 31'i yeşil — ilk OPS-2 deploy'u).**
(1) `production environment: HARBOR_CI_ROBOT_NAME HARBOR_CI_ROBOT_SECRET TS_OAUTH_CLIENT_ID
TS_OAUTH_SECRET KUBE_CONFIG present`. (2) Taban imaj
`registry.everva.com.tr/dockerhub/library/golang:1.26.9-bookworm@sha256:d9c68c2c…453c` (index)
üzerinden çekildi; Harbor `dockerhub`'da `library/golang` altında görünen artifact index'in
linux/amd64 çocuk manifest'i `sha256:bcef992b77b1…9eff` (etiketsiz; Docker Hub index'inin listesiyle
eşleştirildi — madde 2'nin yöntemi). Erişim kapısı iki imaj için `ok … WITH` ve `refused … (HTTP 401
UNAUTHORIZED)`. (3) `tailnet: tailscale 1.94.2, backend Running, tags tag:ci, ephemeral absent` ve
`cluster API reachable at 10.43.0.1:443 (attempt 1)`. (4) `pull-secret: secret/harbor-pull in tappa is
kubernetes.io/dockerconfigjson with a credential for registry.everva.com.tr`. (5) Harbor
`tappa/tappa` ve `tappa/tappa-migrate`'te `sha-34648e3d5c94`. (6) Sunucu pod'u
`registry.everva.com.tr/tappa/tappa:sha-34648e3d5c94`, `ps=harbor-pull`, imageID
`registry.everva.com.tr/tappa/tappa@sha256:69953bdf…`; migrate Job'ı `…/tappa-migrate:sha-34648e3d5c94`;
hiçbir pod'da waiting reason yok. (7) Bu koşu tailnet'te cihaz **bırakmadı** (yerel `tailscale
status`) — ephemeral beklentisiyle tutarlı; ephemeral bayrağı runner'dan ölçülemedi (kapı:
`ephemeral absent`). OAuth öncesi auth key'li koşulardan yedi eski
`github-runnervm8k3b6*` cihazı offline duruyor; kullanıcı konsoldan siler. (8) Log'da e-posta yok;
iki `tskey` eşleşmesi ön kontrol betiğinin önek denetimi **metni**, değer değil. (9) Ön koşul
yapıldı: deploy'dan önce depo düzeyindeki **altı** secret silindi (`TS_AUTHKEY`, `KUBE_CONFIG`,
`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, `TS_OAUTH_CLIENT_ID`, `TS_OAUTH_SECRET`) — depo düzeyinde
secret kalmadı, `production`'da beş; `01-rbac.yaml` deploy'dan önce uygulandı (SubjectAccessReview:
deployer `harbor-pull`/`tappa-secrets` üzerinde yalnız `get`). **Kullanıcıda bekleyen:** Docker Hub
token'ının ve eski Tailscale auth key'inin iptali, yedi eski `github-*` cihazın silinmesi.

**Ölçülmeyen, sayılı:** KEP-2535'in Harbor'la davranışı (değişmemiş `harbor-pull` ile yeniden başlayan
pod Harbor'a gitmeden açılır mı; k8s-2 kapalıyken ne olur) bu kümede **ölçülmedi** — sınır
**[Harbor k8s-2'ye bağlı]**. Güvenli yarısı istenirse ölçülebilir: k8s-2 ayaktayken
`kubectl --context hetzner-k8s-1 -n tappa rollout restart deployment/tappa` + `rollout status` yeşil olmalı; k8s-2'yi kapatarak
ölçmek bir kesinti denemesidir ve ayrı bir karardır.

### OPS-2 geri dönüş — OPS-2 öncesi Docker Hub imajlarına

Prod'un OPS-2 öncesi son imajı `docker.io/atknatk/tappa:sha-f577d743fc5b` (ana oturumun bildirdiği,
2026-10-19). Harbor'a **taşınmadan** ona dönülebilir mi — ölçülen ve ölçülemeyen ayrı:

- **`kubectl rollout undo` — evet, taşımadan.** Geri dönülen ReplicaSet'in pod şablonu Deployment'a
  **olduğu gibi** kopyalanır (Kubernetes'in `rollout undo` tanımı); OPS-2 öncesi ReplicaSet'in şablonu
  `docker.io/atknatk/tappa:sha-…` ve **`imagePullSecrets`'sız** — yani kimliksiz, eskiden nasıl koştuysa
  öyle: düğümde duruyorsa önbellekten, durmuyorsa Docker Hub'dan anonim. Ağaçtan ölçüldü: OPS-2 öncesi
  `20-app.yaml` (`f577d74`) `imagePullSecrets` taşımıyor ve `deploy.yml`'i yalnız etiketi değiştiriyordu
  (`sed "s|:deploy-placeholder|:sha-$SHORT|g"`). Hangi revizyonun hangisi olduğunu *"Elle deploy /
  rollback"*'in ReplicaSet komutu basar; `--to-revision=<N>` ile seç.
- **`kubectl set image … tappa=docker.io/atknatk/tappa:sha-f577d743fc5b` — evet, taşımadan.** OPS-2
  şablonundaki `imagePullSecrets: harbor-pull` `docker.io` için kimlik taşımaz; kubelet kimliği
  registry adresine göre seçer, eşleşen yoksa anonim çeker (bu kümede **ölçülmedi** — Kubernetes'in
  belgelenen davranışı).
- **Deploy hattıyla f577d74'ü yeniden sevk etmek — Harbor'a yeni bir imaj olarak.** `deploy.yml`
  `main`'in HEAD'ini inşa eder; eski bir commit'i sevk etmenin yolu `main`'de geri almaktır (`git
  revert`), o da **yeni** bir commit ve Harbor'da **yeni** bir `sha-*` etiketidir — Docker Hub'daki
  imajı kullanmaz, taşıma gerektirmez. ⚠️ OPS-2 öncesi `deploy.yml`'i geri getirmek ise **çalışmaz**
  (depo düzeyindeki `DOCKERHUB_*` ve `TS_AUTHKEY` silindikten sonra ilk adımda düşer — kapalı yönde).
- **`registry.everva.com.tr/tappa/tappa:sha-f577d743fc5b` — yok.** Harbor'daki ilk etiket OPS-2'nin ilk
  koşusununkidir; bu adrese `set image` `manifest unknown` → `ImagePullBackOff` verir. Harbor'a taşımak
  **gerekirse** (ör. Docker Hub depoları silinecekse), etiketi koruyarak — imaj içeriği aynı kalır,
  `verify-image.sh` kapısını Docker Hub'da zaten geçmişti:

  İtmek `robot$tappa-ci`'nin ya da bir proje yöneticisinin kimliğini ister (`tappa` projesine push
  yetkisi olan yalnız odur; `k8s-pull` itemez). 🔴 **`docker login` ile DEĞİL** — macOS'ta kimliği
  keychain'e **kalıcı** yazar (operatör adımı 4 (c), ölçüldü). Blok (c)'nin kalıbını kullanır: secret
  `read -rs` ile okunur, geçici bir `config.json`'a yazılır, yalnız `push` o dosyayla (`DOCKER_CONFIG`)
  koşar, dosya en sonda silinir. `DOCKER_HOST` geçerli docker bağlamından alınır — `DOCKER_CONFIG`
  değişince docker CLI o dizinin bağlamını (yani hiçbirini) okur ve Docker Desktop'ın soketini
  bulamayabilir. İki imaj da (sunucu ve migration), etiket korunarak:

  ```bash
  umask 077
  D=$(mktemp -d)
  X=$(docker context inspect -f '{{.Endpoints.docker.Host}}')
  printf 'Harbor user (robot$tappa-ci or a project admin): '; read -r U
  printf 'its secret: '; read -rs S; echo
  printf '{"auths":{"registry.everva.com.tr":{"auth":"%s"}}}\n' "$(printf '%s:%s' "$U" "$S" | base64 | tr -d '\n')" > "$D/config.json"
  unset S
  for R in tappa tappa-migrate; do docker pull "docker.io/atknatk/${R}:sha-f577d743fc5b" && docker tag "docker.io/atknatk/${R}:sha-f577d743fc5b" "registry.everva.com.tr/tappa/${R}:sha-f577d743fc5b" && DOCKER_CONFIG="$D" DOCKER_HOST="$X" docker push "registry.everva.com.tr/tappa/${R}:sha-f577d743fc5b" && echo "${R} pushed" || echo "${R} NOT pushed"; done
  rm -rf "$D"
  ```

🔴 **Önkoşul — Docker Hub'daki etiketin bugün de KİMLİKSİZ çekilebildiğini ÖLÇ** (bu bölüm yazılırken
dış ağ yoktu; ölçülmedi). `rollout undo` ve `set image` bu imajı **kimliksiz** çeker, yani ölçü de
kimliksiz olmalı. 🔴 **Boş bir `DOCKER_CONFIG` ile `docker manifest inspect` bunu ÖLÇMEZ** (OPS-2
düzeltme turu 1): dizinde kimlik yoksa docker CLI varsayılan kimlik yardımcısını (macOS'ta keychain)
kendisi seçer, yani bu makine Docker Hub'a giriş yapmışsa **private** bir depo da okunur. Ölçü
curl'le, Docker Hub'ın kimliksiz token'ıyla yapılır (`-q`: hiçbir `~/.curlrc` kimlik eklemez); iki
satır da `HTTP 200` basmalı (`401` = private ya da yok, `404` = etiket yok):

```bash
for R in atknatk/tappa atknatk/tappa-migrate; do T=$(curl -q -fsS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:${R}:pull" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'); curl -q -sS -o /dev/null -w "${R}: HTTP %{http_code}\n" -H "Authorization: Bearer $T" -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' "https://registry-1.docker.io/v2/${R}/manifests/sha-f577d743fc5b"; done
```

**Taban imajın proxy'si çalışmazsa** (Harbor'ın `dockerhub` uç noktası Docker Hub'a ulaşamıyor, proxy
projesi silinmiş): *"Build both images"* düşer, kümeye dokunulmaz. Geri dönüş **aynı digest'i**
Docker Hub'dan kimliksiz çekmektir — `deploy.yml`'deki `GO_BUILD_IMAGE` satırını `Dockerfile`'ın
varsayılanına (`golang:1.26.10-bookworm@sha256:5b1f78db…b505`; 2026-10-21'den önce 1.26.9) çevirip `main`'e bir commit; içerik
digest'le aynı kalır, bedeli OPS-2'nin ilk turundaki anonim bütçe riskidir (sınır 12 (e)). İki
satırı birlikte değiştiren pin `cmd/tappa/deployregistry_test.go`'dadır
(`TestPackaging_TheBuildPullsItsBaseImageThroughHarborByDigest`) — geri dönüş onu da günceller.

⚠️ **Docker Hub depolarını silmek ya da private yapmak bu bölümün ilk iki yolunu kapatır** — ve
sınır 18'in kalan yarısını (eski etiketlerin herkese açık olması) kapatır. İkisi aynı kararın iki
yüzü: OPS-2 yeterince koştuktan sonra verilir, önce gerekirse son OPS-2 öncesi imaj yukarıdaki gibi
Harbor'a taşınır.

---

## M14-02 — admin identity starting counts (ölçüldü 2026-10-22)

ADR 0031'in (tek kimlik, çok işletme) backfill'i (§5.8) ve contract ön koşulu (*"tek aktif owner'ı
bağlanmamış tenant = 0"*, K-3, plan M14-20a) **hangi sayılardan başladığını** bilmek için canlı
veritabanında bir kez koşturuldu.

**Neye yeniden kullanılır, neye kullanılmaz.** Bu sorgu yalnız **M14-06'dan (backfill) önce** yeniden
koşturulup bu tabloyla karşılaştırılır. `05f`, `05h` ve `08*` backfill'i **taklit eder** (`pick` /
`linked` CTE'leri: en eski aktif, yoksa en eski); `identity_id`, `unlinked_at`, M14-18'in bağlarını ya
da uzlaştırmayı (M14-06b) **hiç okumaz** — kişi o işletmeye bağlandıktan sonra da `05f` yine 1 basar.
M14-19 ve M14-20a ön kontrolleri gerçek bağı **ayrı bir sorguyla** ölçer (bağlı = `identity_id IS NOT
NULL AND unlinked_at IS NULL`; reddedilmiş — `unlinked_at` damgalı — satırların tenant'ları ayrı
sayılır, ADR 0031 §5.12, plan M14-20a). `05f` yalnız backfill öncesi bir simülasyondur.

**Contract ön koşulunun başlangıç değeri `05f` = 1'dir.** Kartın lafzı (*"tek aktif owner'ı bir
çok-satırlı grupta olan tenant"*) `05e` = 2'yi verir; o kullanılmadı, çünkü backfill çok satırlı
grubun bir satırını zaten bağlar — o grubun tenant'larından biri bağlı başlar.

**Kim koşturur.** Kullanıcı / orkestratör koşturur; 2026-10-22 ölçümü kullanıcı yetkisiyle,
salt-okur yapıldı (*"ajan `kubectl` koşmaz"* kuralına bu kart için kullanıcı izni).

**Yalnız sayı.** Sorgunun döndürdüğü her satır sabit bir etiket ve bir sayıdır: e-posta, ad, `id`,
tenant adı, token ya da digest veritabanından çıkmaz; ne ekrana, ne bu belgeye, ne log'a yazılır
(CLAUDE.md §7). **Parolaların aynı olup olmadığı ölçülemez:** `password_hash` tuzlu bcrypt'tir, aynı
parolanın iki digest'i farklıdır — `08k` yalnız *bayt-aynı* digest'i (bir satırdan ötekine
kopyalanmış) sayar, parola eşitliğini değil.

**Erişim (sır yok).** Tailnet'ten, `tappa_owner` ile (canlı kümede superuser — FORCE RLS satır
gizlemez; şema sorgusuyla ölçüldü: `rolsuper = t`, `rolbypassrls = t`), SQL **stdin'den dosyayla**,
`READ ONLY` transaction, sonunda `ROLLBACK`:

```bash
kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
    sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -U tappa_owner -d tappa -v ON_ERROR_STOP=1 -At' \
    < m14-02-measure.sql
```

Ölçüm anında şema: goose sürümü **64**; `admin_sessions`'ta `expires_at` **yok** (`id, tenant_id,
admin_user_id, token_hash, created_at, last_used_at, revoked_at`) — sunucu tarafında oturumu bitiren
yalnız `revoked_at` ve yöneticinin `status`'üdür (`internal/adminauth/cookie.go` 12 saatlik
`Max-Age` yalnız ipucu). `admin_users_tenant_email_key` = `UNIQUE (tenant_id, email) WHERE email IS
NOT NULL` (citext) → aynı kişinin aynı tenant'ta iki satırı **yapısal olarak imkânsız**; `02g` bunu
ayrıca sayar. `admin_users_password_hash_is_bcrypt` canlıda `NOT VALID` (00018'in kararı) — `08j`
eski satırların kalıba uyup uymadığını sayar, çünkü `admin_identities.password_hash` aynı CHECK'i
taşıyacak ve backfill seçilen satırın digest'ini kopyalayacak.

**Gruplama anahtarı backfill'inkiyle bire bir değil.** Sorgu `NULLIF(btrim(lower(email)), '')` ile
gruplar; backfill (`00073`) citext eşitliğiyle ve yalnız dolu e-postayla (`email IS NOT NULL AND
btrim(email) <> ''`) gruplar — boş ya da yalnız boşluktan oluşan adresi ikisi de dışarıda bırakır. İkisi
artık yalnız baş-son boşluklu adreslerde ayrışır (sorgu kırpar, backfill kırpmaz): **`01d` ≠ 0 ise
`08a` / `05f` backfill'le uyuşmaz.** 2026-10-22'de 0'dı, yani aşağıdaki sayılar backfill'in
göreceğiyle aynıdır. (Kaydedilen sayılar bu SQL'in çıktısı olduğu için SQL değiştirilmedi.)

`m14-02-measure.sql` (metnin tamamı):

```sql
-- M14-02: admin identity starting counts. Aggregates only: every row returned is a
-- constant label and a count. No e-mail, name, id, tenant name, token or digest leaves
-- the database. tappa_owner is a superuser on the live cluster, so FORCE RLS does not
-- hide rows. READ ONLY transaction, rolled back.
BEGIN TRANSACTION READ ONLY;

WITH r AS (
  SELECT a.id, a.tenant_id, a.role, a.status, a.created_at, a.password_hash,
         NULLIF(btrim(lower(a.email::text)), '') AS k,
         a.email IS NULL                                            AS email_null,
         a.email IS NOT NULL AND btrim(a.email::text) = ''          AS email_blank,
         a.email IS NOT NULL AND a.email::text <> btrim(a.email::text) AS email_padded,
         a.password_hash ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$' AS bcrypt_ok
    FROM admin_users a
),
g AS (                                    -- one row per filled e-mail group
  SELECT k, count(*) AS n, count(DISTINCT tenant_id) AS nt,
         count(*) FILTER (WHERE status = 'active') AS nactive,
         count(DISTINCT password_hash) AS ndigest,
         count(DISTINCT email_raw) AS nspell
    FROM (SELECT r.*, a.email::text AS email_raw FROM r JOIN admin_users a ON a.id = r.id) x
   WHERE k IS NOT NULL
   GROUP BY k
),
pick AS (                                 -- ADR 0031 §5.8: oldest active, else oldest
  SELECT r.*, g.n,
         row_number() OVER (PARTITION BY r.k
                            ORDER BY (r.status = 'active') DESC, r.created_at, r.tenant_id, r.id) = 1
           AS picked
    FROM r JOIN g USING (k)
),
linked AS (                               -- row would be linked by the backfill
  SELECT id FROM pick WHERE picked
),
ao AS (                                   -- active owners, with backfill fate
  SELECT r.tenant_id, r.k, (r.id IN (SELECT id FROM linked)) AS will_link,
         coalesce(g.n, 0) AS group_n
    FROM r LEFT JOIN g USING (k)
   WHERE r.role = 'owner' AND r.status = 'active'
),
t AS (
  SELECT tn.id,
         (SELECT count(*) FROM admin_users a WHERE a.tenant_id = tn.id) AS nrows,
         (SELECT count(*) FROM ao WHERE ao.tenant_id = tn.id) AS nao,
         (SELECT count(*) FROM ao WHERE ao.tenant_id = tn.id AND ao.will_link) AS nao_linked,
         (SELECT count(*) FROM ao WHERE ao.tenant_id = tn.id AND ao.group_n > 1) AS nao_multi,
         (SELECT count(*) FROM ao WHERE ao.tenant_id = tn.id AND ao.k IS NULL) AS nao_noemail
    FROM tenants tn
),
s AS (
  SELECT s.*, a.status AS admin_status
    FROM admin_sessions s JOIN admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
)
SELECT label, value FROM (VALUES
 -- 6. totals
 ('06a tenants_total',                       (SELECT count(*) FROM tenants)),
 ('06b admin_users_total',                   (SELECT count(*) FROM r)),
 -- 1. e-mail presence
 ('01a rows_email_filled',                   (SELECT count(*) FROM r WHERE k IS NOT NULL)),
 ('01b rows_email_null',                     (SELECT count(*) FROM r WHERE email_null)),
 ('01c rows_email_blank_string',             (SELECT count(*) FROM r WHERE email_blank)),
 ('01d rows_email_with_outer_whitespace',    (SELECT count(*) FROM r WHERE email_padded)),
 -- 2. group sizes over lower(email)
 ('02a groups_total',                        (SELECT count(*) FROM g)),
 ('02b groups_size_1',                       (SELECT count(*) FROM g WHERE n = 1)),
 ('02c groups_size_2',                       (SELECT count(*) FROM g WHERE n = 2)),
 ('02d groups_size_3plus',                   (SELECT count(*) FROM g WHERE n >= 3)),
 ('02e rows_in_multirow_groups',             (SELECT coalesce(sum(n), 0) FROM g WHERE n > 1)),
 ('02f max_group_size',                      (SELECT coalesce(max(n), 0) FROM g)),
 ('02g groups_with_rows_sharing_a_tenant',   (SELECT count(*) FROM g WHERE nt < n)),
 ('02h groups_with_case_variant_spellings',  (SELECT count(*) FROM g WHERE nspell > 1)),
 -- 3. status x role
 ('03a rows_active_owner',                   (SELECT count(*) FROM r WHERE status = 'active'   AND role = 'owner')),
 ('03b rows_active_manager',                 (SELECT count(*) FROM r WHERE status = 'active'   AND role = 'manager')),
 ('03c rows_disabled_owner',                 (SELECT count(*) FROM r WHERE status = 'disabled' AND role = 'owner')),
 ('03d rows_disabled_manager',               (SELECT count(*) FROM r WHERE status = 'disabled' AND role = 'manager')),
 ('03e rows_disabled_in_multirow_groups',    (SELECT count(*) FROM pick WHERE n > 1 AND status = 'disabled')),
 -- 4. panel sessions (admin_sessions has no expires_at: server-side, "live" = not revoked)
 ('04a sessions_total',                      (SELECT count(*) FROM s)),
 ('04b sessions_not_revoked',                (SELECT count(*) FROM s WHERE revoked_at IS NULL)),
 ('04c sessions_not_revoked_admin_active',   (SELECT count(*) FROM s WHERE revoked_at IS NULL AND admin_status = 'active')),
 ('04d sessions_live_used_within_12h',       (SELECT count(*) FROM s WHERE revoked_at IS NULL AND admin_status = 'active'
                                                AND coalesce(last_used_at, created_at) > now() - interval '12 hours')),
 ('04e sessions_live_used_within_30d',       (SELECT count(*) FROM s WHERE revoked_at IS NULL AND admin_status = 'active'
                                                AND coalesce(last_used_at, created_at) > now() - interval '30 days')),
 ('04f admins_with_a_live_session',          (SELECT count(DISTINCT admin_user_id) FROM s WHERE revoked_at IS NULL AND admin_status = 'active')),
 -- 5. tenants and their active owners
 ('05a tenants_with_0_admin_rows',           (SELECT count(*) FROM t WHERE nrows = 0)),
 ('05b tenants_with_0_active_owners',        (SELECT count(*) FROM t WHERE nao = 0)),
 ('05c tenants_with_1_active_owner',         (SELECT count(*) FROM t WHERE nao = 1)),
 ('05d tenants_with_2plus_active_owners',    (SELECT count(*) FROM t WHERE nao > 1)),
 ('05e only_owner_in_multirow_group',        (SELECT count(*) FROM t WHERE nao = 1 AND nao_multi = 1)),
 ('05f only_owner_not_linked_by_backfill',   (SELECT count(*) FROM t WHERE nao = 1 AND nao_linked = 0)),
 ('05g   of_which_owner_has_no_email',       (SELECT count(*) FROM t WHERE nao = 1 AND nao_linked = 0 AND nao_noemail = 1)),
 ('05h tenants_with_owners_none_linked',     (SELECT count(*) FROM t WHERE nao >= 1 AND nao_linked = 0)),
 -- backfill starting values (ADR 0031 §5.8)
 ('08a backfill_identities_to_create',       (SELECT count(*) FROM g)),
 ('08b backfill_rows_left_unlinked_w_email', (SELECT count(*) FROM pick WHERE NOT picked)),
 ('08c   of_which_active',                   (SELECT count(*) FROM pick WHERE NOT picked AND status = 'active')),
 ('08d   of_which_active_owner',             (SELECT count(*) FROM pick WHERE NOT picked AND status = 'active' AND role = 'owner')),
 ('08e groups_with_2plus_active_rows',       (SELECT count(*) FROM g WHERE nactive > 1)),
 ('08f groups_with_0_active_rows',           (SELECT count(*) FROM g WHERE nactive = 0)),
 ('08g multirow_groups_picked_row_is_owner', (SELECT count(*) FROM pick WHERE picked AND n > 1 AND role = 'owner')),
 ('08h multirow_groups_picked_row_is_mgr',   (SELECT count(*) FROM pick WHERE picked AND n > 1 AND role = 'manager')),
 ('08i picked_rows_failing_bcrypt_check',    (SELECT count(*) FROM pick WHERE picked AND NOT bcrypt_ok)),
 ('08j all_rows_failing_bcrypt_check',       (SELECT count(*) FROM r WHERE NOT bcrypt_ok)),
 ('08k multirow_groups_one_identical_digest',(SELECT count(*) FROM g WHERE n > 1 AND ndigest = 1))
) v(label, value)
ORDER BY label;

ROLLBACK;
```

**Sonuçlar — 2026-10-22, canlı (`hetzner-k8s-1`, goose 64):**

| Etiket | Anlamı | Sayı |
|---|---|---|
| `06a` | tenant | **4** |
| `06b` | `admin_users` satırı | **4** |
| `01a` | dolu e-postalı satır | **4** |
| `01b` / `01c` / `01d` | e-postasız (`NULL`) / boş dize / baş-son boşluklu | 0 / 0 / 0 |
| `02a` | `lower(email)` grubu (= backfill'in yaratacağı kimlik, `08a`) | **3** |
| `02b` / `02c` / `02d` | tam 1 / tam 2 / 3+ satırlı grup | **2 / 1 / 0** |
| `02e` / `02f` | çok satırlı gruplardaki satır / en büyük grup | 2 / 2 |
| `02g` | aynı tenant'ta iki satırı olan grup | 0 |
| `02h` | yalnız harf farkıyla yazılmış grup | 0 |
| `03a` / `03b` | aktif owner / aktif manager | **4** / 0 |
| `03c` / `03d` / `03e` | `disabled` owner / `disabled` manager / çok satırlı grupta `disabled` | 0 / 0 / 0 |
| `04a` | `admin_sessions` satırı (hepsi) | 52 |
| `04b` / `04c` | iptal edilmemiş / iptal edilmemiş ve yöneticisi aktif (= sunucuya göre canlı) | **40** / **40** |
| `04d` / `04e` | canlı ve son 12 saatte / son 30 günde kullanılmış | 1 / 33 |
| `04f` | canlı oturumu olan yönetici | 3 |
| `05a` / `05b` | hiç yöneticisi olmayan / aktif owner'ı 0 olan tenant | 0 / 0 |
| `05c` / `05d` | tam 1 / 2+ aktif owner'lı tenant | 4 / 0 |
| `05e` | tek aktif owner'ı çok satırlı bir grupta olan tenant | **2** |
| `05f` | tek aktif owner'ı backfill'de **bağlanmayacak** tenant (contract ön koşulunun başlangıç değeri) | **1** |
| `05g` | `05f`'nin e-postasız owner yüzünden olanı | 0 |
| `05h` | aktif owner'ı var, hiçbiri bağlanmayacak tenant | 1 |
| `08a` | backfill'in yaratacağı kimlik | 3 |
| `08b` / `08c` / `08d` | e-postalı ama bağlanmayacak satır / bunun aktifi / aktif owner'ı | 1 / 1 / 1 |
| `08e` / `08f` | 2+ aktif satırlı grup / hiç aktif satırı olmayan grup (en eskiye düşer) | 1 / 0 |
| `08g` / `08h` | çok satırlı grupta seçilen satır owner / manager | 1 / 0 |
| `08i` / `08j` | bcrypt kalıbına uymayan seçilen satır / bütün satırlar | 0 / 0 |
| `08k` | bayt-aynı tek digest'li çok satırlı grup | 0 |

**Okuma.** Canlıda tek bir kişi iki işletmenin aktif owner'ıdır (bir grup, iki tenant, iki aktif
owner satırı, digest'leri farklı). Backfill o grubun en eski aktif satırını bağlar; öbür tenant'ın
**tek** aktif owner'ı bağlanmamış kalır → contract ön koşulu **1**'den başlar ve kişinin o işletmeye
bir kez kimlik yoluyla bağlanmasıyla (M14-18) 0'a iner. Öbür iki tenant'ın owner'ı tek satırlı
grupta, backfill'le bağlanır. Aktif owner'ı olmayan, e-postasız, `disabled` ya da bcrypt kalıbına
uymayan satır yok. `internal/adminauth/resetrequest.go:52`'nin *"1 675"* adresi (birden çok satıra
çözülen) geliştirme veritabanının sayısıdır; canlıda bu sayı **1**'dir (`02c` + `02d`), en büyük grup 2.

---

## Elle deploy / rollback

> 🔴 **Bu bölümdeki her `kubectl` TAILNET'TEN koşar (T134, 2026-10-17).** Küme API'si
> internete kapalı; kubeconfig'in sunucusu `https://10.43.0.1:443` olmalı ve makine
> tailnet'te, subnet rotası (`10.43.0.0/16`) kabul edilmiş olmalı (`tailscale up
> --accept-routes`). Operatör proxy'si (`*.ts.net`) kullanılmaz — operatör adımı 5.
> M11 deploy'u (2026-10-17) tam bu yoldan elle yapıldı.

> 🔴 **ÖNCE ŞUNU ÖLÇ: BU BÖLÜM HARBOR'I TARİF EDİYOR (OPS-2); KÜMENİN HANGİ REGISTRY'DEN
> KOŞTUĞU KÜMEYE SORULUR, AĞACA DEĞİL.** Ağaçtaki pod spec'leri
> `registry.everva.com.tr/tappa/...` + `imagePullSecrets: harbor-pull` diyor. OPS-2'nin ilk deploy'u
> inene kadar küme **Docker Hub** koşar (ana oturumun bildirdiği son durum, 2026-10-19:
> `docker.io/atknatk/tappa:sha-f577d743fc5b`, `imagePullSecrets` yok). Ölç, iddia etme — Deployment
> ve geri dönülebilecek her ReplicaSet (`rollout undo` bir öncekine döner):
> ```bash
> kubectl -n tappa get deployment tappa \
>   -o jsonpath='{.spec.template.spec.containers[0].image}{"  ps="}{.spec.template.spec.imagePullSecrets[*].name}{"\n"}'
> kubectl -n tappa get rs -l app.kubernetes.io/component=server \
>   -o jsonpath='{range .items[*]}{.metadata.annotations.deployment\.kubernetes\.io/revision}{"  "}{.spec.template.spec.containers[0].image}{"  ps="}{.spec.template.spec.imagePullSecrets[*].name}{"\n"}{end}'
> ```
> Çıktıyı şöyle oku:
> - `registry.everva.com.tr/tappa/... + ps=harbor-pull` → OPS-2 inmiş: aşağıdaki komut olduğu gibi;
> - `docker.io/atknatk/... + ps yok` → OPS-2 öncesi (ya da ona dönülmüş): etiketi `docker.io/atknatk/tappa`
>   ile yaz — public, kimliksiz çekilir (*"OPS-2 geri dönüş"*).
>
> ⚠️ **Etiketin registry'de VAR olduğunu da ölç** — `set image` onu denetlemez (aşağıda). Harbor
> depo başına **son 30 push**'u tutar; Docker Hub'da OPS-2 öncesi etiketler depolar silinene dek
> durur. Tailnet'teki, `harbor-pull` ile aynı robotun kimliğine sahip bir makineden ya da Harbor
> arayüzünden (Projects → `tappa` → depo → Artifacts) bakılır.

> 🔴 **ÜÇÜNCÜ KOMUT AYRI BİR KARAR — BU YÜZDEN KENDİ BLOĞUNDA.** İlk blok (`set image` +
> `rollout status`) yer tutucu taşıdığı için kopyalayanı **durdurur**, `rollout undo`
> **durdurmaz**; onu koşmadan önce bloğun **altındaki** uyarıyı oku. (Üçlü bir zamanlar
> bilerek tek blokta duruyordu, sırayla okunsun diye; T136'dan beri `undo` ayrı bir
> blokta — ilk bloğu kopyalamak onu artık sürüklemez. `undo` bir kurtarma değil,
> koşullu bir harekettir.)

⚠️ ETİKETİN KAYNAĞINI YUKARIDAKİ ÖLÇÜMDEN AL: OPS-2 inmişse registry.everva.com.tr/tappa/tappa,
OPS-2 öncesi bir imaja dönüyorsan docker.io/atknatk/tappa.

🔴 YER TUTUCU SESSİZCE KABUL EDİLİR — ölçüldü (çevrimdışı, --local):
`kubectl set image ... tappa='<registry>/atknatk/tappa:sha-<12hex>'` çıktısı BİREBİR
`<registry>/atknatk/tappa:sha-<12hex>`; istemci tarafında imaj referansının doğrulaması YOKTUR. Yani
yanlış yazılmış bir etiket buradan geçer ve arızayı ancak kubelet bildirir (`ImagePullBackOff`).

```bash
kubectl -n tappa set image deployment/tappa tappa=<image>:sha-<12hex>
kubectl -n tappa rollout status deployment/tappa
```

🔴 ÜÇÜNCÜ KOMUTU KOŞMADAN ÖNCE ALTTAKİ UYARIYI OKU.

- `kubectl -n tappa rollout undo deployment/tappa`: bir önceki imaja

```bash
kubectl -n tappa rollout undo deployment/tappa
```

> 🔴 **`rollout undo` NEREYE DÖNDÜĞÜNÜ ŞABLONUYLA BİRLİKTE GETİRİR.** Geri dönülen ReplicaSet'in pod
> şablonu Deployment'a **olduğu gibi** kopyalanır — imaj **ve** `imagePullSecrets` dahil. İki dünya:
> - **Harbor ReplicaSet'ine** (`ps=harbor-pull`) dönmek: `harbor-pull` değişmediyse imaj düğümdeki
>   kopyadan açılır, Harbor'a gidilmez (KEP-2535: kayıtlı kimlik aynı). Robot döndürüldüyse ya da
>   imaj düğümde yoksa Harbor (k8s-2) ayakta olmalı; 30 push'tan eskiyse Harbor'da da yoktur —
>   sınır **[Harbor k8s-2'ye bağlı]**.
> - **OPS-2 öncesi ReplicaSet'e** (`docker.io/...`, `ps` yok) dönmek: kimliksiz, Docker Hub'dan ya da
>   düğümün önbelleğinden — **Docker Hub depoları public ve etiket duruyorsa** (*"OPS-2 geri dönüş"*).
> (OPS-2 öncesi bu kutu `secret/ghcr`'ın bayat token'ı yüzünden `401` uyarısı taşıyordu; o Secret'ı
> yazan adım M8-02 FAZ D'de ağaçtan, OPS-2'de de tüm akıştan çıktı.)
>
> ⚠️ **Her hâlükârda rollout'u izle** — geri alma başka sebeplerle de takılabilir:
> ```bash
> kubectl -n tappa rollout undo deployment/tappa
> kubectl -n tappa rollout status deployment/tappa --timeout=120s || \
>   kubectl -n tappa get pod -o wide
> ```

> 🔴 **E-POSTA AÇIKKEN ESKİ BİR İMAJA DÖNMEK — ÖNCE ConfigMap, SONRA İMAJ (2026-10-09'dan
> beri iki akış `email`).** ConfigMap imajla birlikte geri **gelmez** ve eski ikili onu
> okur. Bu ikililer o değerle **açılmayı reddeder** (ölçüldü, iki commit'in `main.go`'su
> `unbuiltDelivery`):
> - `a8d38ff` (EM-7B, davet kanalı) **öncesi** → `TAPPA_INVITE_DELIVERY=email` ile açılmaz;
> - `a2ed96f` (EM-5, sıfırlama kanalı) **öncesi** → `TAPPA_RESET_DELIVERY=email` ile de açılmaz.
>
> `replicas: 1` + `maxUnavailable: 0`: yeni pod açılmaz, `rollout status` zaman aşımına
> uğrar — geri alma **olay anında** takılır. Bu yüzden böyle bir SHA'ya `rollout undo` /
> `set image`'dan **ÖNCE** ConfigMap'te `TAPPA_INVITE_DELIVERY: "panel"` (hedef `a2ed96f`
> öncesiyse ayrıca `TAPPA_RESET_DELIVERY: "none"`) yaz ve uygula, **sonra** imajı geri al:
>
> `05-config.yaml` düzeltildikten sonra (davet `panel`, gerekiyorsa reset `none`):
>
> ```bash
> kubectl --context hetzner-k8s-1 -n tappa apply -f deploy/k8s/05-config.yaml
> kubectl --context hetzner-k8s-1 -n tappa get configmap tappa-config \
>   -o jsonpath='{.data.TAPPA_RESET_DELIVERY} {.data.TAPPA_INVITE_DELIVERY}{"\n"}'
> ```
>
> Ancak bundan **SONRA** yukarıdaki `set image` ya da `rollout undo`.
>
> Ayrıca: e-postayı açmanın önkoşullarının kodu `d69087b`'de (EM-7C) tamamlanır (sıra:
> `a2ed96f` EM-5 → `b935f47` EM-7A kesici → `a8d38ff` EM-7B → `d69087b` EM-7C tavan).
> Daha eski ama reddetmeyen bir imaj **açılır**, yalnız korumasız gönderir: `d69087b`
> öncesi alıcı başına tavan yok, `b935f47` öncesi süreç geneli kesici de yok. Bu aralığa
> dönerken de önce `none` / `panel` önerilir. Bugünkü canlı (`e95a717`) EM-7A/B/C'yi içerir; açılışta
> sorun yok, risk yalnız geri dönüştedir. Geri alma sonrası ConfigMap'i yeniden `email`'e
> çevirmek de bir deploy kararıdır (*"Transactional e-mail"*, 3. adım).

> 🔴 **Rollback şemayı geri almaz.** `goose down` bu iş akışında **yoktur** ve
> bilinçlidir: geri alınabilir olmak (`-- +goose Down` dolu) ile *otomatik olarak
> geri alınmak* aynı şey değildir. Bir migration'ı geri almak, `transactions`
> immutable olduğu için (§4.3) veri kaybı anlamına gelebilir — el ile, ölçerek
> yapılır.

---

## Yedek ve geri yükleme

> 🔴 **ÖNCE ŞUNU ÖLÇ: BU BÖLÜM AĞACI TARİF EDİYOR; KÜMEDE CronJob OLUP OLMADIĞI AYRI
> BİR SORU.** Ağaç ile küme bu dosyada daha önce üç kez ayrıştı ve üçünde de ayrımı
> yazmayan cümle yanlış çıktı. Teşhise **kümeye sorarak** başla — ilk satır CronJob'u
> sorar (yoksa: operatör adımı 9 yapılmamış), ikincisi son gecelerin Job'larını, üçüncüsü
> betik ConfigMap'ini ve hedef Secret'ını:
> ```bash
> kubectl -n tappa get cronjob tappa-backup
> kubectl -n tappa get job -l app.kubernetes.io/component=backup
> kubectl -n tappa get configmap tappa-backup-scripts secret/tappa-backup-target
> ```
> **Ölçüm, 2026-08-16 (salt okuma):** `kubectl -n tappa get cronjob` → **`No resources
> found`**. Yani bugün kümede yedek **yoktur**; ağaçtaki her şey operatör adımı 9'u
> bekliyor. Sınır **[yedek kümede HENÜZ YOK]**.

### Ne alınıyor, ne zaman, ve içi nasıl doğrulanıyor

`deploy/k8s/50-backup.yaml` her gece **02:30 Malta** saatinde (`timeZone:
Europe/Malta`, yani yaz/kış kayması yok) tek bir pod koşar: `wait-for-postgres` →
`dump-and-verify` → `ship`. İkincisi düşerse üçüncüsü **hiç başlamaz**, yani
doğrulanmamış bir dump hedefe **gidemez**.

`ship` bitince — yeşil ya da kırmızı — sonucu healthchecks.io'nun `tappa-backup`
kontrolüne bildirir (T116, operatör adımı 10): yeşilse *"yaşıyor"*, kırmızıysa `/fail`.
İkincisi hiç başlamadıysa bildirim **yoktur** ve kontrol 1 saatlik toleransın sonunda
uyarır. Bildirim yedeğin sonucunu **değiştirmez**: Secret yoksa, adres reddedilirse ya da
servis cevap vermezse log'a tek satır düşer, çıkış kodu aynı kalır.

Her koşu iki dosya bırakır: `tappa-<UTC damgası>.sql.gz` ve yanında bir
`.manifest`. Manifest'te sır yoktur; içinde sha256, yedek anı, sunucu sürümü,
`goose` sürümü, tablo/politika/GRANT sayıları, **tablo tablo satır sayıları** ve (T45'ten
beri) **fonksiyon ve rol envanteri** var: `public`'teki her fonksiyonun sahibi,
`SECURITY DEFINER`'ı, `search_path`'i ve etkin `EXECUTE` listesi; `public`'te bir şeyin
sahibi ya da yetkilisi olan her rolün süper kullanıcı/`BYPASSRLS`/üyelik nitelikleri.
`pg-restore-verify.sh` geri yüklenen kataloğu satır satır buna karşı okur.

🔴 **`pg_dump`'ın bu şemadaki asıl tuzağı ölçüldü, ve tahmin edilenden farklı çıktı.**
Her tenant tablosu **`ENABLE` ve `FORCE ROW LEVEL SECURITY`** taşıyor (§6) ve
politikalar `app.tenant_id` okuyor. `FORCE` tablo **sahibine** de uygulanır, yani GUC
set edilmemiş bir bağlantı her tenant tablosunda **sıfır satır** görür — bu repodaki
veritabanında ölçüldü: `tappa_owner` 231832 `transactions` satırı görüyor, `tappa_app`
**0**. `pg_dump`'ın buna tepkisi:

| komut | sonuç |
|---|---|
| `pg_dump -U tappa_app --data-only -t transactions` | **exit 1**, `ERROR: query would be affected by row-level security policy` — **fail-closed** ✅ |
| ...aynısı `--enable-row-security` ile | **exit 0**, 966 bayt, **0 satır** — *sessizce boş yedek* 🔴 |
| ...aynısı, `app.tenant_id` bir tenant'a set edilmişken | **exit 0**, 231832 satırın **17262'si** — *sessizce kısmi yedek* 🔴 |

Yani tehlike gerçek ama mekanizma tek bir bayrağa bağlı: `pg_dump` varsayılan olarak
`row_security = off` kuruyor ve bypass edemeyen bir rol **hata alıyor**. `pg-backup.sh`
bunu üç katmanda kapatıyor, ikisi **yapısal**: (1) `--enable-row-security` hiç
geçilmiyor; (2) iş, bağlanan rol `rolsuper` ya da `rolbypassrls` **değilse** pg_dump'a
hiç ulaşmadan duruyor; (3) bitmiş dump'ın satırları sayılıp canlıyla karşılaştırılıyor
ve *"canlıda satır var, dump'ta yok"* olan her tablo işi kırmızıya çeviriyor.

⚠️ **Hiçbir kontrol *"sıfırdan fazla satır"* istemiyor** — çünkü sağlıklı bir yeni
kurulum **boştur** ve öyle bir eşik her doğru yedeği kırmızı yapardı. Ölçüldü: 19
tablo / 0 satırlık taze bir şema **exit 0** veriyor (12 KiB'lik bir arşiv). Kırmızı
olan iki hâl **eksiklik**: tablosu olmayan bir veritabanı (yanlış `PGDATABASE`) ve
canlıda dolu / dump'ta boş bir tablo.

**Ölçülmüş süreler** — 19 tablo, **2 118 896 satır**, 546 MiB düz / **120 MiB**
gzip'li (bu reponun kendi seed'lenmiş veritabanı; pilot verisi bunun binde biri
mertebesinde olacak):

| Adım | Süre |
|---|---|
| `pg_dump` | 85 sn |
| doğrulama (5 geçiş) + gzip | ~3 dk |
| **toplam `dump-and-verify`** | **4 dk 33 sn** |
| boş bir şemanın tamamı | < 1 sn |
| `psql` ile geri yükleme | **37–39 sn** |
| `pg-restore-verify.sh` | **23 sn** |

**T45 (2026-10-09), goose 34** — 24 tablo, **6 779 780 satır**, 1,81 GB düz / **408 MB**
gzip'li (geliştirme veritabanı; Docker Desktop, 4 CPU / 3,8 GB VM, staging bir
bind-mount): `pg_dump` **158 sn** · **toplam `dump-and-verify` 648 sn** · `ship`
(crypt + yerel S3 API, 78 parçalı çoklu yükleme) **11 sn** · hedeften geri alma
**25 sn** · `psql` ile geri yükleme **102 sn** · `pg-restore-verify.sh` **64 sn**.

### Son yedek ne durumda — iddia değil, komut

```bash
kubectl -n tappa get job -l app.kubernetes.io/component=backup \
  -o custom-columns=NAME:.metadata.name,DONE:.status.succeeded,FAIL:.status.failed,START:.status.startTime
kubectl -n tappa logs job/<job> -c dump-and-verify | tail -5
```

Kümenin **dışından** aynı soru: healthchecks.io panelinde `tappa-backup` kontrolünün son
sinyali ve durumu (operatör adımı 10). `ship`'in son satırı hangi sinyalin gittiğini söyler:
`alert signal sent (success|failure, HTTP 2xx)`, `alert signal skipped` (Secret yok) ya da
`NOT delivered (<sınıf>)`.

Hedefteki kopyayı **kendi makinenden** (kümeye hiç dokunmadan) listelemek — config
emanetten operatör adımı 9(b)'nin yoluna (`$HOME/.config/tappa/`, repo dışı, 0600)
geri konmuş olarak:

```bash
rclone --config "$HOME/.config/tappa/backup.rclone.conf" lsl tappa-backup:prod
```

(`<bucket>` artık bu yolda **yok**: kova `crypt`'in `remote =` satırındadır — operatör
adımı 9(b), R2 örneği. Eski `tappa-backup:<bucket>/tappa-prod` yazımı kovasız bir
`crypt` kökü varsayıyordu ve S3/R2'de kanarya adımında kırmızıya düşer — ölçüldü.)

⚠️ `rclone check` bir `crypt` remote'ta **`ERROR : No common hash found`** basar ve
**bu bir arıza değildir** (ölçüldü, rclone v1.71.2: aynı çıktıda `0 differences found`
ve exit 0; `--ignore-checksum` bunu bastırmıyor). Hüküm veren şey çıkış kodudur.

---

### 🔴 GERİ YÜKLEME — ve ÖNCE BUNU OKU: GERİ YÜKLEME ÇOĞU ZAMAN YANLIŞ CEVAPTIR

Bir geri yükleme **zamanı geri alır**. Yedek anından sonra yazılmış **her** mesai
kaydı, o kayıtlarla birlikte gider — ve `transactions` §4.3 gereği **immutable**,
yani o satırlar başka hiçbir yerden yeniden üretilemez. Bu, §4.6'nın (*"kayıt asla
kaybolmaz"*) doğrudan ihlalidir. Yani:

| Belirti | Geri yükleme doğru cevap mı |
|---|---|
| Uygulama açılmıyor, veritabanı sağlam (`/readyz` 503, `pg_isready` OK) | **HAYIR.** Bu bir deploy sorunu — *"Olay müdahalesi"* bölümüne git |
| Yanlış bir migration şemayı bozdu ama satırlar duruyor | **Muhtemelen hayır.** Önce ileri düzeltme (yeni migration) dene; geri yükleme aradaki tüm dokunuşları siler |
| Bir müdür yanlış kayıt girdi | **KESİNLİKLE HAYIR.** Düzeltme = **yeni kayıt + `audit_log`** (§4.3). Geri yükleme bir düzeltme aracı değildir |
| Toplu bir `DELETE` / `DROP` gerçekten satır sildi | **Belki** — ama önce kayıp aralığını ölç (aşağıda), sonra karar ver |
| Node'un diski gitti, veritabanı yok | **EVET** — tek yol bu, ve kayıp aralığı = yedek anından arızaya kadar olan her şey |

🔴 **YEDEK ANINI BİLMEDEN GERİ YÜKLEME YAPILMAZ.** O an manifest'in
`started_at` satırıdır ve `pg-backup.sh` her koşuda son satır olarak da basar.

---

#### 🔴 ÖNCE HANGİ YOLDASIN — İKİ AYRI PROSEDÜR VAR, VE YANLIŞINI SEÇMEK PAHALI

```bash
kubectl -n tappa get statefulset tappa-postgres
kubectl -n tappa exec statefulset/tappa-postgres -- pg_isready -U tappa_owner -d tappa
```

| Cevap | Yol |
|---|---|
| StatefulSet ayakta, `accepting connections` (veritabanı **var**, içeriği bozuk) | **A YOLU** — aşağıdaki 8 adım |
| StatefulSet yok / PVC gitti / node yeniden kuruldu (**instance YOK**) | 🔴 **B YOLU** — *"B YOLU"* başlığı, ayrı ve ayrıca numaralı |

⚠️ **Bu ayrım bir tur boyunca yoktu ve denetim bunu bloklayan saydı, haklı olarak.**
Karar tablosundaki tek kesin **EVET** (*"node'un diski gitti"*) tam olarak
**B YOLU**'dur, ama numaralı prosedürün tamamı A YOLU'nu — yani hasarlı instance'ın
hâlâ var olmasını — varsayıyordu (adım 4 `CREATE DATABASE`, adım 5 bozuk veritabanından
kayıp aralığı, adım 7 `RENAME` takası). 04:00'te node'u gitmiş bir operatör adımların
yarısının uygulanamadığını görüp **doğaçlar** — ve doğaçlanan yol, ayrıcalık artığı
üreten yolun ta kendisidir.

---

#### A YOLU — instance ayakta, veritabanı bozuk (adım adım)

**1. Kesintinin var olduğunu ve şeklini ölç.** İki dış kanıt:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/healthz
curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/readyz
```

**2. 🔴 HİÇBİR ŞEYİ SİLMEDEN ÖNCE, BOZUK VERİTABANININ KENDİSİNİ YEDEKLE.** Bu adım
§4.6'nın karşılığıdır: geri yükleyeceğin an ile şimdi arasındaki kayıtları **ancak
elinde tutarsan** sayabilir ve sonradan yeniden girebilirsin.

```bash
kubectl -n tappa create job tappa-backup-preresto --from=cronjob/tappa-backup
kubectl -n tappa wait --for=condition=complete job/tappa-backup-preresto --timeout=3600s
```

Veritabanı bu işi koşamayacak kadar bozuksa (`dump-and-verify` düşüyorsa) **dur ve
düşün**: geri yüklemek, sayamadığın bir kaybı kabul etmek demektir.

**3. Yedeği indir ve bütünlüğünü doğrula.** Döküm **repo dışına**, `mktemp -d`'nin
verdiği dizine iner (çalışanların mesai kaydıdır; depo PUBLIC). Sonraki adımlar **aynı
kabukta** `$RESTORE`'u kullanır; yeni bir kabuğa geçersen değişkeni yeniden ata. 5.
adımın kayıp aralığı CSV'si de buraya yazılır (`$RESTORE/lost-window.csv`) ve 8. adımda
okunur — bu yüzden silme **takastan (7) sonra değil, yeniden giriş (8) bittikten sonra**:
`rm -rf "$RESTORE"` döküm, manifest ve CSV'yi birlikte götürür. Config emanetten
operatör adımı 9(b)'nin yoluna konur.

- `STAMP=<damga>`: indirdiğin dizinin adı, ör. 20260816T023000Z
- `have=$(sha256sum "$G" | awk '{print $1}')`: macOS'ta yoksa: shasum -a 256 "$G"

```bash
STAMP=<damga>
RESTORE=$(mktemp -d)
rclone --config "$HOME/.config/tappa/backup.rclone.conf" copy "tappa-backup:prod/$STAMP" "$RESTORE/"

M=$RESTORE/tappa-$STAMP.manifest
G=$RESTORE/tappa-$STAMP.sql.gz
want=$(awk '/^sha256 /{print $2}' "$M")
have=$(sha256sum "$G" | awk '{print $1}')
if [ "$want" = "$have" ]; then echo "sha256 OK $want"; else echo "🔴 MISMATCH manifest=$want file=$have"; fi
awk '/^started_at|^goose_version|^rows_total_dump|^tables/' "$M"
```

> 🔴 **BU KOMUT BİR TUR BOYUNCA `sha256sum -c <<<"…"` İDİ VE OPERATÖRÜN KENDİ
> MAKİNESİNDE ÇALIŞMIYORDU.** Ölçüldü (bu makine, `sha256sum (Darwin) 1.0`):
> here-string ile → **`usage: sha256sum [-bctwz] [files ...]`, exit 1**; aynı komut
> Debian + GNU coreutils'te → `OK`. Yani **sağlıklı bir yedek**, kesinti anında
> *"komut bozuk / yedek bozuk"* gibi okunan bir exit 1 üretiyordu. Bu, bu dosyanın
> 118–121. satırlarında bedeli **zaten ödenmiş** olan kalıptır: *yerel araç ≠ hedef
> araç*, ve prova Linux konteynerlerinde koşulduğu için bu satır operatörün
> platformunda **hiç koşulmamıştı**. Yeni biçim iki değişkeni karşılaştırıyor;
> **her iki lezzette de** ölçüldü (macOS **ve** GNU: aynı hash, exit 0).
> ⚠️ Ve `$STAMP` **bilerek elle yazılıyor**: eski komut `./restore/*.manifest` glob'u
> kullanıyordu ve dizinde **iki damga** varsa `awk` iki hash basıp sessizce yanlış
> eşleştiriyordu (ölçüldü, iki dosyayla). Bir geri yükleme tek bir damgaya karşı
> yapılır; hangisi olduğunu komut da bilmeli.

**4. 🔴 CANLI VERİTABANININ ÜSTÜNE DEĞİL, YANINA GERİ YÜKLE.** Aynı Postgres
instance'ında **yeni ve boş** bir veritabanı yarat, oraya yükle. Bu üç şeyi birden
verir: bozuk veritabanı **durmaya devam eder** (§4.6), kayıp aralığı **ölçülebilir**
hâle gelir, ve — ölçüldü — ayrıcalıklar **doğru** çıkar.

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
  psql -U tappa_owner -d postgres -c "CREATE DATABASE tappa_restore OWNER tappa_owner;"'

gzip -dc "$RESTORE/tappa-<damga>.sql.gz" | kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -U tappa_owner -d tappa_restore'
```

> 🔴 **NEDEN AYNI INSTANCE'TA YENİ BİR VERİTABANI VE NEDEN TAZE BİR POD DEĞİL —
> ÖLÇÜLDÜ, VE SONUÇ SEZGİYE TERS.** Taze bir Postgres pod'una geri yüklemek
> `tappa_app`'e **fazladan 31 yetki** veriyor, `public.transactions` üzerinde
> **UPDATE ve DELETE dahil** — yani §4.3'ün iki kemerinden birini sessizce çözüyor.
> Mekanizma: `01-roles.sql` initdb'de `ALTER DEFAULT PRIVILEGES … GRANT SELECT,
> INSERT, UPDATE, DELETE ON TABLES TO tappa_app` koşuyor, dolayısıyla geri yükleme
> sırasındaki **her `CREATE TABLE` dördünü de otomatik veriyor**; `pg_dump` ise ACL'i
> PostgreSQL'in **yerleşik** varsayılanına göre hesapladığı için fazlalığı geri alan
> `REVOKE`'ları hiç yazmıyor. **Yeni bir veritabanında bu olmuyor**, çünkü
> `pg_default_acl` **veritabanı başına** bir katalogdur: ölçüldü, `tappa`'da 2 satır,
> yeni veritabanında **0**. Geri yükleme sonrası dump'ın kendi son iki satırı onları
> yerine koyuyor (yine 2) — ⚠️ **ve o "yerine koyduğu" şey GENİŞ varsayılandır**
> (`GRANT SELECT,INSERT,DELETE,UPDATE`), yani geri yükleme bittiğinde hedefteki
> varsayılan **yine `arwd`**. Var olan tabloları etkilemiyor (ölçüldü: 45 yetki,
> `tags` UPDATE/DELETE hâlâ false); etkilediği şey **sonraki DDL**'dir. Bu yüzden
> her iki yolun da **son adımı** varsayılanı yeniden daraltıyor.
> Sayılarla: kaynak **45** tablo yetkisi · yeni veritabanına geri yükleme **45**
> (fazla 0, eksik 0) · taze pod'a geri yükleme **76**.
>
> ⚠️ **Taze bir pod'a geri yüklemek ZORUNDAYSAN** (node gitti, instance yok) bu
> bölümü değil **B YOLU**'nu izle: orada askıya alma adımı **1. adımdır**, bir dipnot
> değil.
>
> ⚠️ **`pg_dump`'ın taşımadığı tek şey ROLLERDİR.** Aynı dump'ta ölçüldü: 18 `ENABLE
> ROW LEVEL SECURITY`, 18 `FORCE ROW LEVEL SECURITY`, 18 `CREATE POLICY`, 105 `GRANT`,
> 12 `REVOKE`, 35 `OWNER TO`, 2 `ALTER DEFAULT PRIVILEGES`, 2 `CREATE EXTENSION` —
> ve **0 `CREATE ROLE`**. Roller küme kapsamlı nesnelerdir; `tappa_app`,
> `tappa_resolver` ve M10'dan beri **`tappa_operator`, `tappa_opdefiner`**
> `scripts/db-init/01-roles.sql`'den, `tappa_owner` `POSTGRES_USER`'dan
> gelir. Aynı instance'a geri yüklerken zaten oradalar; taze bir pod'da init script'i
> onları yaratıyor. **Bu yüzden geri yükleme yolu bu repodan başlar, dump'tan değil.**
> (goose 34 şemasında, T45: 23 `ENABLE` / 23 `FORCE`, 20 `CREATE POLICY`, 240 `GRANT`,
> 27 `REVOKE`, 60 `OWNER TO` — 15'i `tappa_opdefiner`'a, 6'sı `tappa_resolver`'a —
> 2 `ALTER DEFAULT PRIVILEGES`, 2 `CREATE EXTENSION`, yine **0 `CREATE ROLE`**.)
>
> ⚠️ **Disk:** iki kopya aynı 20Gi PVC'de yan yana durur. `kubectl -n tappa exec
> statefulset/tappa-postgres -- df -h /var/lib/postgresql/data` ile önce bak.

**5. 🔴 KAYIP ARALIĞINI ÖLÇ — VE BU, TAKASTAN ÖNCEKİ SON GERİ DÖNÜŞ NOKTASIDIR.**
Manifest'in `started_at` değeri yedek anıdır; ondan sonra yazılmış her `transactions`
satırı geri yüklemeyle **yok olur**.

> 🔴 **SQL BURADA `-c` İLE DEĞİL, HEREDOC İLE VERİLİYOR — VE BU BİR TERCİH DEĞİL,
> ÖLÇÜLMÜŞ BİR DÜZELTME.** Bu bölümün ilk yazımı SQL'i `sh -c '… psql -c "… IN
> (''tappa'',''tappa_restore'') …"'` biçiminde taşıyordu ve **bozuktu**: tek tırnak
> içinde `''` tırnağı kapatıp yeniden açar, yani kabuk psql'e `IN (tappa,
> tappa_restore)` gönderiyordu → `ERROR: column "tappa" does not exist`. Birebir
> yeniden üretildi. Heredoc'ta tırnaklar **olduğu gibi** geçer; `$STAMP` ve
> `$(date …)` **bu makinede**, `$POSTGRES_PASSWORD` ise tek tırnak sayesinde
> **konteynerde** çözülür — yani parola hiçbir zaman senin kabuğuna girmez (§4.7).

```bash
STAMP=$(awk '/^started_at/{print $2}' "$RESTORE"/*.manifest)
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<SQL
SELECT count(*) || ' kayit | ' || coalesce(min(created_at)::text,'-')
       || ' .. ' || coalesce(max(created_at)::text,'-')
  FROM transactions WHERE created_at > '$STAMP';
SQL
```

Sıfırdan büyükse, **takastan önce** o satırları dışarı al — geri yükleme onları
silmeyecek (bozuk veritabanı duruyor) ama yeniden girilmeleri gerekecek:

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -At -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' \
  > "$RESTORE/lost-window.csv" <<SQL
COPY (
  SELECT id, tenant_id, employee_id, location_id, department_id, occurred_at, created_at,
         type, verdict, channel, practice, note, entered_by
    FROM transactions WHERE created_at > '$STAMP'
) TO STDOUT CSV HEADER;
SQL
```

> 🔴 **`SELECT *` DEĞİL, VE EKSİK SÜTUNLAR BİLEREK EKSİK — §4.2/§4.7.** İlk yazım
> `SELECT *` diyordu; ölçüldü: `public.transactions` **`gps_lat numeric(9,6)`** ve
> **`gps_lng numeric(9,6)`** taşıyor, yani o CSV bir olay anında operatörün diskine
> çalışan başına **tam koordinat** düşürüyordu — §4.2 GPS'i *"yalnız tap anında
> okunur"* diye sınırlarken, düz metin bir dosya olarak dışarı çıkarıyordu. Dışarıda
> bırakılanlar ve neden: **`gps_lat`/`gps_lng`** (konum kanıtı) · **`source_ip`**
> (ağ kanıtı) · **`tag_uid`/`ctr`/`sun_valid`** (SUN kanıtı) · **`trust`**,
> **`ip_match`/`gps_match`**, `policy_*`, `matched_sid` (karar motorunun çıktısı).
> **Hiçbirine ihtiyaç yok:** adım 8 yeniden girişi `channel='manual'` ile yaptırıyor
> ve manuel bir kaydın zaten SUN'ı, IP'si ve GPS'i yoktur — o satırlar yeniden
> girilirken **yeniden hesaplanmaz, hiç doğmaz**. Kalan sütunlar *"kim, nerede, ne
> zaman, giriş mi çıkış mı"* sorusunu tam olarak cevaplıyor.
>
> 🔴 **`-v ON_ERROR_STOP=1` DE BİR DÜZELTMEDİR, SÜS DEĞİL.** Ölçüldü: bayraksız bir
> `psql` hatalı bir ifadede **exit 0** verir, bayrakla **exit 3**. `> "$RESTORE/lost-window.csv"`
> biçiminde bayraksız bir `COPY` hatası **boş bir CSV + exit 0** üretirdi ve operatör
> bunu *"kayıp yok"* diye okuyup adım 8'i (yeniden giriş) **atlardı**.

🔴 **Bu CSV yine de çalışanların mesai kaydıdır**: bu yüzden çalışma dizinine değil
3. adımın repo dışındaki `$RESTORE` dizinine (`mktemp -d`, 0700) yazılır; 8. adım bitince
dökümle birlikte silinir (3. adımın silme notu), ve kimseye e-postayla gönderilmez
(§4.7 / GDPR). Yeniden girişleri `channel='manual'` +
`entered_by` ile yapılır ve raporlarda **ayrı görünür** (§5) — bu bir kusur değil,
kaydın gerçekte nasıl doğduğunun dürüst hâlidir.

**6. Geri yüklenen kopyayı DOĞRULA — takastan önce.**

🔴 **Önce punch akışının sayaçlarını ileri at (M11 API-2, ADR 0028 §5).** Döküm her tenant'ın
sayacını **yedek anındaki** değerle getirir; yedekten sonra yazılıp entegratörce okunmuş olaylar bu
kopyada yok, ama entegratörün imleci onların ötesinde. Uygulama bu sayaçtan yazmaya başlarsa yeni
olaylar entegratörün geçtiği `seq`'leri yeniden kullanır ve `seq > imleç` onları **hiç görmez**.
Uygulama yazmadan önce, bir kez:

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa_restore' <<'SQL'
UPDATE public.feed_counters SET last_seq = last_seq + 1000000;
SQL
```

Doğrulama aracı bunu geri yüklenen verinin kendisinden denetler (her tenant'ın sayaç satırı var ve
son olayının en az `1000000` ötesinde) ve atlanmışsa reddeder.

**Eski manifest'li yedek** (M11 API-2 öncesi bir `scripts/pg-backup.sh`'nin yazdığı; manifest'te
`feed_counters <n>` başlığı yok). Araç bu yedeğin 6. bölümünü **reddeder**, çünkü sayaçların **bu**
geri yüklemede ileri atıldığını dökümün kendi sayaç değerleriyle kanıtlayamaz. Önce yeni bir yedek
al. Felakette o yedek **tek** yedekse, punch akışını bir insan doğrular:

1. Yukarıdaki `UPDATE public.feed_counters SET last_seq = last_seq + 1000000;` bloğunu uygulama
   **kapalıyken**, bu geri yükleme için **bir kez** koş. Fazladan bir koşu yalnız boşluğu büyütür
   (entegratörün imleci zarar görmez); hiç koşmamak olayları kaybettirir.
2. Elle denetle, iki sorgunun ikisi de `0` dönmeli:

   ```sql
   SELECT count(*) FROM public.tenants s
    WHERE NOT EXISTS (SELECT 1 FROM public.feed_counters c WHERE c.tenant_id = s.id);
   SELECT count(*) FROM public.feed_counters c
    WHERE c.last_seq < coalesce((SELECT max(p.seq) FROM public.punch_events p WHERE p.tenant_id = c.tenant_id), 0) + 1000000;
   ```

3. Aracın **öteki** bütün bölümleri yine `ok` vermeli; kabul edilen tek `FAIL` 6. bölümün
   *"no 'feed_counters' header"* satırıdır.
4. Olay kaydına yaz: yedeğin damgası, `UPDATE`'in koştuğu an, koşturan kişi, iki sorgunun sonucu.
   Bu kayıt, aracın kanıtlayamadığı adımın kanıtıdır.

Boş bir veritabanının (henüz tenant yok) yedeği bu yola girmez: yeni `pg-backup.sh` onun
manifest'ine de `feed_counters 0` yazar ve araç geçer.

```bash
PGHOST=<postgres> PGDATABASE=tappa_restore PGUSER=tappa_owner \
PGPASSWORD=<owner> TAPPA_APP_PASSWORD=<app> \
  scripts/pg-restore-verify.sh "$RESTORE/tappa-<damga>.sql.gz" "$RESTORE/tappa-<damga>.manifest"
```

Ne ölçtüğü ve neden bu araç olmadan yapılmaması gerektiği: satır satır manifest
karşılaştırması, politika/`FORCE` sayıları, `goose` sürümü, **dump'ın ilan ettiği
yetki kümesiyle birebir karşılaştırma**, ve `tappa_app` olarak **TCP üzerinden**
gerçek bir oturumla iki davranış sondası — GUC'suz **0** satır, GUC'lu tenant'ın
satırları, ve `transactions` üzerinde UPDATE/DELETE'in **`42501` ile** (yani
*yetkiyle*, tetikleyiciyle değil) reddedilmesi.

> 🔴 **NEDEN HATA KODUNU ASSERT EDİYOR.** Bozuk geri yüklemede UPDATE yine
> reddedilir — ama `transactions_no_mutation` **tetikleyicisi** tarafından. Yani
> *"satırı değiştirebiliyor muyum? hayır"* diye bakan bir insan, kemerlerinden birini
> kaybetmiş bir veritabanını **temiz** sanar. Fark yalnız hata kodunda görünür.
> Araç, `WHERE false` ile hiçbir satıra dokunmadan sorar (tetikleyici `FOR EACH ROW`,
> yani ateşlemez ve cevabı maskeleyemez) ve üç sonucu ayırır: `42501` doğru · hatasız
> geçme = yetki kemeri gitmiş · başka bir şey = bilinmiyor, ve bilinmiyor geçer not
> değildir.

**7. Takas — ve bozuk veritabanı SİLİNMEZ.**

- `kubectl -n tappa scale deployment/tappa --replicas=0`: bağlantıları kes

```bash
kubectl -n tappa scale deployment/tappa --replicas=0
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d postgres' <<SQL
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname IN ('tappa','tappa_restore') AND pid <> pg_backend_pid();
ALTER DATABASE tappa RENAME TO tappa_damaged_$(date -u +%Y%m%d);
ALTER DATABASE tappa_restore RENAME TO tappa;
SQL
```

🔴 VE VARSAYILANI YENIDEN DARALT — dump onu az önce genişletti (aşağıdaki not).

- `curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/readyz`: 200 bekleniyor

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE UPDATE, DELETE ON TABLES FROM tappa_app;
SQL

kubectl -n tappa scale deployment/tappa --replicas=1
kubectl -n tappa rollout status deployment/tappa
curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/readyz
```

> 🔴 **O SON `ALTER DEFAULT PRIVILEGES` NEDEN VAR — VE NEDEN `REVOKE UPDATE, DELETE`,
> dördü birden değil.** Dump'ın **kendi son satırları** varsayılanı yeniden
> genişletiyor (`GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO tappa_app`), yani geri
> yükleme biter bitmez hedefteki varsayılan **`arwd`** olur. Ölçüldü, dört adımda:
> dar init → **`ar`** · geri yükleme sonrası → **`arwd`** · o anda yaratılan yeni bir
> tablo `tappa_app`'e **`arwd`** verir · bu tek ifadeden sonra → **`ar`**, ve yeni bir
> tablo **`ar`** alır. **Var olan tablolara dokunmuyor** (aynı ölçümde: 45 yetki,
> `tags` UPDATE/DELETE **false**, `aes_key_ref` UPDATE **false**, sekans varsayılanı
> `rU` değişmedi). `SELECT, INSERT` **bilerek bırakılıyor** — `01-roles.sql`'in
> kastettiği taban yetki odur.
> ⚠️ **Bu atlanırsa ürün bozulmaz** ve tam olarak bu yüzden yazılı: 20 migration'ın
> hepsi istemediğini zaten açıkça `REVOKE` ediyor. Bozulan şey **bir sonraki**
> migration yazarının güvendiği vaattir — `scripts/db-init/01-roles.sql` *"unutulan
> bir GRANT gürültülü patlar"* diyor ve o vaat **yalnız dar varsayılanlı** bir
> veritabanında geçerli.

Ölçüldü: iki `ALTER DATABASE RENAME` toplam **399 ms**, ve takastan sonra doğrulama
aracı aynı veritabanında **PASS** veriyor. `tappa_damaged_<tarih>` **yerinde kalır** —
5. adımın CSV'si onun kopyasıdır, ama asıl kayıt hâlâ duruyor ve §4.6 ancak öyle
karşılanır. Diski geri istediğinde `DROP DATABASE` bir **karardır**, temizlik değil.

**8. Kayıp aralığını yeniden gir.** 5. adımın CSV'sindeki (`$RESTORE/lost-window.csv`)
her kayıt panelden manuel kayıt olarak girilir (`channel='manual'`, `entered_by` dolu).
Otomatik bir toplu `INSERT` **bilerek yazılmadı**: bu satırlar hukuki kayıt ve kimin
girdiği görünmelidir. Son kayıt girilip panelde görüldükten sonra `rm -rf "$RESTORE"`
(3. adımın notu): döküm, manifest ve bu CSV birlikte silinir.

---

#### 🔴 B YOLU — INSTANCE YOK (node gitti, PVC gitti): taze bir pod'a geri yükleme

🔴 **A YOLU'nun hiçbir adımı burada uygulanamaz** ve en önemlisi: **kayıp aralığını
ölçecek bir kaynak yok.** Kayıp, tanımı gereği *"yedek anından arızaya kadar olan her
şey"*tir ve bunu **ancak yedeğin `started_at`'i ile arızanın saati** arasından
tahmin edebilirsin. Bunu bir kayıp beyanı olarak yaz; bir kaza olarak değil.

**B1. Postgres'i normal yoldan ayağa kaldır — ve init script'leri KOŞSUN.**
Roller (`tappa_app`, `tappa_resolver`, `tappa_operator`, `tappa_opdefiner`) dump'ta
**yoktur**, bu repodan gelir — ve **geri yüklemeyi yaptığın checkout'un**
`01-roles.sql`'inden: M10 öncesi bir checkout'un init'i operatör rollerini yaratmaz.
Ölçüldü (T45, operatör bloğu çıkarılmış bir init'le): B3'ün `ON_ERROR_STOP=1`'li
geri yüklemesi ilk satırda `ERROR: role "tappa_opdefiner" does not exist`, **exit 3**,
0 tablo — gürültülü, doğru. Bayrak **olmadan** ise psql 138 hatayla **exit 0** veriyor
ve 15 `SECURITY DEFINER` `op_*` fonksiyonu **süper kullanıcının** malı kalıyor;
`pg-restore-verify.sh` bunu artık adıyla reddediyor (*"functions differ from the
source"*, *"roles differ from the source"*).

- `kubectl apply -f deploy/k8s/01-rbac.yaml`: cluster-admin ile, bir kez

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/01-rbac.yaml
```

tappa-secrets'ı emanetten geri yükle (operatör adımı 2)

```bash
kubectl -n tappa create configmap tappa-db-init \
  --from-file=01-roles.sql=scripts/db-init/01-roles.sql \
  --from-file=02-app-password.sh=deploy/k8s/postgres-init/02-app-password.sh \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f deploy/k8s/10-postgres.yaml
kubectl -n tappa rollout status statefulset/tappa-postgres
```

**B2. 🔴 VARSAYILAN YETKİLERİ ASKIYA AL — GERİ YÜKLEMEDEN ÖNCE. BU ADIM ATLANIRSA
`tags` ÜZERİNDEKİ REPLAY KORUMASI (§4.4) SIFIRLANABİLİR HÂLE GELİR.**

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE USAGE, SELECT ON SEQUENCES FROM tappa_app;
SQL
```

> **Neden bu adım burada birinci sınıf bir adım.** `01-roles.sql` artık varsayılanı
> `SELECT, INSERT`'e daraltıyor (dosyanın kendi ölçümü orada), yani bu adım
> atlansa bile `tags UPDATE/DELETE` ve `aes_key_ref` **kapalı** kalır — ölçüldü.
> **Ama sıfıra inmiyor:** daraltılmış init + askıya alma **yok** → **50** yetki
> (5 fazla, aralarında `legal_documents` üzerinde **tablo düzeyi SELECT**, ki o da
> kendi sütun listesini ezer). Daraltılmış init + bu adım → **45/0/0**, yani
> kaynakla birebir. İki kemer birlikte.
> **Ve dump'ın son iki satırı varsayılanı geri koyuyor** (`pg_default_acl` → 2),
> yani bu adım kalıcı bir sakatlama **değildir**. ⚠️ **Ama "doğru" kelimesi burada
> bir tur boyunca GENİŞ anlamına geliyordu ve öyle yazılmıyordu:** dump'ın koyduğu
> varsayılan `SELECT,INSERT,DELETE,UPDATE`'tir. B3'ün son komutu onu yeniden
> daraltıyor; o komut atlanırsa geri yüklenmiş veritabanı **üretimin bugünkü hâline**
> döner ve `01-roles.sql`'in *"unutulan GRANT gürültülü patlar"* vaadi orada
> **geçerli olmaz**.

**B3. Yedeği indir, sha256'yı doğrula, geri yükle.**

🔴 BLOĞUN SON KOMUTU B2'NİN AYNASI — dump varsayılanı yeniden genişletti, aynı blok onu geri
daraltır; geri yüklemeyle **birlikte** yapıştırılsın diye ayrı bir blokta değil.

```bash
RESTORE=$(mktemp -d)
rclone --config "$HOME/.config/tappa/backup.rclone.conf" copy tappa-backup:prod/<damga> "$RESTORE/"
gzip -dc "$RESTORE/tappa-<damga>.sql.gz" | kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -U tappa_owner -d tappa'

kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE UPDATE, DELETE ON TABLES FROM tappa_app;
SQL
```

> **B2 geri yüklemeden ÖNCE, bu ondan SONRA, ve ikisi farklı işler yapıyor.** B2 var
> olan tabloların ayrıcalık artığını engelliyor; bu ise **gelecekteki** tabloların
> varsayılanını düzeltiyor. Ölçümü A YOLU 7. adımın altındaki notta.

**B4. 🔴 DOĞRULA — VE BU YOLDA ARAÇ İSTEĞE BAĞLI DEĞİL.** A YOLU'nda yanında
karşılaştıracağın bozuk bir veritabanı var; burada **yok**, yani geri yüklemenin
doğru olduğuna dair tek kanıt bu araçtır.

Önce — uygulama açılmadan — punch akışının sayaçlarını ileri at (A YOLU 6. adımın gerekçesi):

```bash
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
UPDATE public.feed_counters SET last_seq = last_seq + 1000000;
SQL
```

```bash
PGHOST=<postgres> PGDATABASE=tappa PGUSER=tappa_owner \
PGPASSWORD=<owner> TAPPA_APP_PASSWORD=<app> \
  scripts/pg-restore-verify.sh "$RESTORE/tappa-<damga>.sql.gz" "$RESTORE/tappa-<damga>.manifest"
```

**Çıkmazsa `exit 0`, uygulamayı açma.** Araç (goose 34'te, T45 provası) 44 tablo +
603 sütun yetkisini dump'ın ilan ettiğiyle, 35 fonksiyonun sahibini /
`SECURITY DEFINER`'ını / `search_path`'ini / `EXECUTE`'unu ve 5 rolün niteliklerini
kaynağın manifest'iyle karşılaştırıyor ve `42501`'i assert ediyor; B2 atlanmışsa
**fazlaları adıyla** listeler. (2026-08-16'daki goose 20 sayısı 45 + 454 idi.)
Doğrulama geçtikten sonra indirilen döküm silinir: `rm -rf "$RESTORE"` (B3'te
`mktemp -d` ile, repo dışında yaratıldı; çalışanların mesai kaydıdır).

⚠️ **B YOLU'ndan sonra operatör yüzeyi kapalı doğar:** `tappa_operator` init'ten
**NOLOGIN** gelir (roller dökümde yoktur, parolalar da) — *"Operator surface (M10
OP-7)"* → *"Geri yüklemeden sonra (B YOLU, taze küme)"*. Doğrulama aracı girişi bilerek
karşılaştırmaz; geri kalan nitelikleri karşılaştırır.

**B5. Uygulamayı aç ve kayıp aralığını İLAN ET.**

```bash
kubectl apply -f deploy/k8s/05-config.yaml
kubectl apply -f deploy/k8s/12-networkpolicy.yaml
kubectl apply -f deploy/k8s/20-app.yaml
kubectl apply -f deploy/k8s/40-ingress.yaml
kubectl -n tappa rollout status deployment/tappa
curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/readyz
```

Sonra **yedeğin `started_at`'i ile arıza saati arasındaki her tap kaybolmuştur** ve
bu, §4.6'nın karşılanamadığı bir aralıktır. Müdürlere aralığı **açıkça** bildir; o
saatlerin kayıtları `channel='manual'` ile yeniden girilir.

**B6. Yedeği yeniden kur** — `50-backup.yaml` ve `tappa-backup-target` bu kümede de
yok (operatör adımı 9). Yedeksiz koşan bir kurtarma, bir sonraki arızada aynı yerde
durur.

### Provanın kendisi — bu prosedür ölçüldü, tarif edilmedi

M8-02'nin kriteri *"geri yükleme **denenmiş**"* diyor. 2026-08-16'da bu reponun kendi
seed'lenmiş veritabanından alınan gerçek bir yedekle, üç kontrollü koşu yapıldı:

| Koşu | Sonuç |
|---|---|
| taze pod'a geri yükleme, düzeltme adımı **yok** (negatif kontrol) | `psql` exit 0, stderr **0 satır**, 19/19 tablo, 2 118 896/2 118 896 satır — **ama 76 yetki (31 fazla)**; doğrulama aracı **exit 1** ve 31'ini adıyla listeledi |
| taze pod'a geri yükleme, düzeltme adımı **var** | **45 yetki** (fazla 0, eksik 0), doğrulama aracı **exit 0** |
| aynı instance'ta yeni veritabanı + `RENAME` takası | **45 yetki**, `pg_default_acl` 2, doğrulama aracı **exit 0** |

Üçünde de `tappa_app` GUC'suz **0** satır gördü ve GUC'lu 17 262 satır gördü, yani
§4.5 geri yüklemeden **sağ çıkıyor** — kaybolan şey §4.3'ün yetki kemeriydi ve onu
yalnız hata kodunu assert eden bir kontrol gördü.

**T45 provası (2026-10-09, goose 34) — zincirin tamamı, betiklerin kendisiyle.**
Geliştirme veritabanı (6 779 780 satır; yalnız okundu, sunucu tarafında
`default_transaction_read_only=on` ile) → `pg-backup.sh` (CronJob'ın konteyner
biçiminde: uid 65532, salt-okur kök) → `pg-backup-ship.sh` → `crypt` + yerel bir S3
API'si (`provider = Cloudflare`, `no_check_bucket = true`; Cloudflare'e istek yok) →
`rclone copy` ile geri alma (A YOLU 3. adımın sha256'sı) → atılabilir bir pod'a B YOLU
(B1 init, B2, B3, B3 daraltma) → `pg-restore-verify.sh`. (Bu ilk koşu 9(d)'nin blokları
**henüz yazılmadan** önce, eşdeğer komutlarla yapıldı; blokların kendisi 2. turda,
yazıldığı gibi ayrıca koşuldu — 9(d)'nin sonundaki not.)

| Adım | Sonuç |
|---|---|
| `pg-backup.sh` | exit 0 · 24/24 tablo · 6 779 780 = 6 779 780 · 35/35 fonksiyon sahibi |
| `pg-backup-ship.sh` | exit 0 · kanarya şifreli kanıtlandı · hedefte adlar opak, her nesne `RCLONE\0\0` ile başlıyor · düz damga/`tappa-` geçen ad **0** |
| geri alma | sha256 manifest'le eşit · indirilen dosya staging'dekiyle **bayt bayt** aynı |
| B YOLU geri yükleme | `psql` exit 0, stderr **0 satır** |
| `pg-restore-verify.sh` | **PASS**: 24 tablo, 20 politika, 23 `FORCE`, goose 34, 6 779 780 satır, 44 + 603 yetki, 35 fonksiyon, 5 rol; GUC'suz 0, GUC'lu 49 867 satır; `42501` |
| bağımsız karşılaştırma (araç dışı) | 24 tablonun her birinde satır sayısı **ve** satır içeriklerinin sıra-bağımsız özeti kaynakla **eşit**; `tags.aes_key_ref` (237 938), `tags.app_key_ref` (1 225) ve `tenant_branding.logo` (3 175) özetleri **eşit** |

Ve aracın yeni bölümünün negatif kontrolü (aynı geri yüklemenin şema-yalnız kopyası):
`op_touch_session`'ın sahibi süper kullanıcı · `search_path` sıfırlanmış · `EXECUTE`
PUBLIC'e · `EXECUTE` `tappa_app`'e · `SECURITY DEFINER` düşmüş · `tappa_operator`
`BYPASSRLS` · `tappa_opdefiner` `SUPERUSER` · `tappa_opdefiner` `tappa_owner`'ın üyesi —
**eski araç sekizine de exit 0**, yenisi sekizine de **exit 1** ve farkı adıyla; her biri
geri alınınca yine exit 0. `pg-backup.sh`'ın 4f kontrolü: `--no-owner` ile alınmış bir
dump'ı eski betik **exit 0** ile gönderiyordu, yenisi *"35 functions … sets the owner
of 0"* ile **exit 1**. Ve 4f'nin sayımı her rutin türünü kapsar: şemaya bir
`PROCEDURE` ve bir `AGGREGATE` eklenince (37 rutin; dump'ta 35 `ALTER FUNCTION`, 1
`ALTER PROCEDURE`, 1 `ALTER AGGREGATE`) yalnız `FUNCTION` sayan 1. tur betiği **exit 1**
veriyordu, bugünkü **exit 0** ve geri yüklemesi `functions: all 37 identical` ile PASS.

---

## Plaket encode — boş çipten duvara

> ### 🔴 BU BÖLÜM BİR PROSEDÜR DEĞİLDİR — BUGÜN ENCODE ARACI YOK
>
> Aşağıdaki "KEK döndürme" bölümü *"bu bölüm bir prosedürdür"* diye açar; **bu
> bölüm bunun tersidir ve fark kasıtlıdır.** Ölçüldü: `cmd/` altında bir encode
> aracı yok, repoda `AuthenticateEV2First` / `ChangeFileSettings` / `ChangeKey`
> uygulayan tek satır Go yok (bu adlar yalnız yorumlarda geçiyor), okuyucu
> donanımı da yok. Yani **bugün bu bölümü açıp adım adım izleyerek plaket encode
> edemezsiniz.** Burada olan şey **kararların ve kısıtların** yazılmasıdır: hangi
> ayar hangi ADR maddesinden geliyor, aracın hangi şekli **alamayacağı**, anahtarın
> süreç içinde nasıl davranmak **zorunda olduğu**. Araç yazıldığında bu bölüm
> onun **kabul kriteri** olur; o zamana kadar aşağıdaki *"Encode aracı …"* cümleleri
> bir davranış tarifi değil, bir **şartname**dir.
>
> **Bu bölüm M8-05'in donanımsız yarısıdır (FAZ A).** Encode ayarları, anahtar
> teslimi, anahtar hijyeni ve baskı **burada, bugün** karara bağlı. Çipe dokunan
> her şey — okuyucu seçimi, gerçek APDU akışı, uçtan uca doğrulama — bölümün
> sonundaki **"FAZ B'ye devredilenler"** listesindedir ve **yazılmadan yapılmaz**.
>
> **Neden bu dosyada.** Aşağıdaki "KEK döndürme" bölümüyle bu bölüm **aynı iki
> nesneyi** anlatıyor (plaketin AES anahtarı · onu saran KEK) ve tarihsel olarak
> tam da bu ikisi birbirine karıştırıldı. Ayrı dosyalara koymak, iki metnin
> sessizce ayrışmasına izin verirdi; aynı dosyada bir bakım turu ikisini birden
> görür. Çapraz atıf tek yönlü değil: aşağıdaki *"Ne döndürülüyor — ve ne
> döndürülMÜyor"* tablosunun **"Dönmez"** satırı buraya, bu bölümün *"Anahtar
> teslimi ve döndürme"* başlığı oraya bakar.

### Q10 — plaketleri KENDİMİZ encode ederiz (karar: 2026-08-18)

**Karar.** Encode'lu tedarikçiden plaket **satın alınmaz**; boş NTAG 424 DNA
çipleri alınır ve anahtar üretimi + SDM yapılandırması **Tappa tarafında** yapılır.

**Gerekçe.** Tedarikçi encode ederse **park geneli anahtarları tedarikçi bilir**.
Bu, [ADR 0003](../docs/adr/0003-sdm-modu-ve-anahtar-yonetimi.md) madde 3'ün
(Q06 — plaket-başına rastgele) açıkça reddettiği **tek-nokta felaketinin** bir kat
yukarı taşınmış hâlidir: master anahtar yerine *tedarikçinin listesi* sızarsa yine
tüm park düşer, üstelik bizim kontrolümüz dışında bir yerde. ADR 0003 madde 5 zaten
*"tedarikçiden gelen varsayılan anahtar üretimde **asla** kullanılmaz"* diyor;
tedarikçinin ürettiği bir anahtar da aynı cümlenin kapsamındadır — bizim
üretmediğimiz anahtar, bizim bilmediğimiz kadar başkasının bildiği anahtardır.

**Bedeli sayılmıştır.** Kendimiz encode etmek bir okuyucu donanımı + bir araç yolu
gerektiriyor (aşağıda) ve plaket başına elle iş ekliyor. Bugünkü ölçek bunu
taşınabilir kılıyor: pilot **tek şube**, tam yayılım KF 9 lokasyon + KM 5
departman mertebesinde. Binlerce plaketlik bir ölçek geldiğinde bu karar yeniden
ölçülür — ama o zaman bile cevap "tedarikçi anahtarı bilsin" değil, "encode
otomasyonu hızlansın" olmalıdır.

### 🔴 Neden "offline script üret, sonra çipe yapıştır" DİYE BİR YOL YOK

Bu, araç seçiminden **önce** gelen bir kısıttır ve emsale aykırıdır: bu repodaki
diğer iki anahtar aracı (`test/fixtures/seedkeys`, `cmd/rotatekek`) **saf
filtrelerdir** — sürücü yok, ağ yok, kimlik bilgisi yok, stdin → stdout. Encode
aracı **bu şekli alamaz.** Ölçüldü, NXP **AN12196 rev. 1.8** üzerinden:

| Adım | Belge | Ne gerektiriyor |
|---|---|---|
| `Cmd.AuthenticateEV2First` (0x71 / 0xAF) | §6.6 tablo 14 | İki geçişli **canlı** challenge: çip `E(K, RndB)` döner, okuyucu `E(K, RndA‖RndB')` gönderir, çip `TI` döner. Oturum anahtarları `RndA` **ve çipin ürettiği** `RndB`'den türer. |
| `Cmd.ChangeFileSettings` (0x5F) | §6.9 tablo 19 | `CommMode.Full`: veri `KSesAuthENC` ile, IV `TI` + `CmdCtr`'den; ardından `KSesAuthMAC` ile MAC. |
| `Cmd.ChangeKey` (0xC4) | §6.16 tablo 26/27 | Aynı oturum anahtarları + `TI` + `CmdCtr`; yeni anahtar `Old ⊕ New ‖ KeyVer ‖ CRC32` olarak **şifreli** gider. |

`RndB` ve `TI` çipten gelir ve her oturumda değişir. Dolayısıyla bir C-APDU
dizisi **önceden hesaplanamaz**; encode aracı okuyucuyla **canlı** konuşmak
zorundadır. Bu, aracın "hangi rolle bağlanıyor" sorusunu **filtre şekliyle
kaçamayacağı** tek anahtar aracımız olduğu anlamına gelir.

⚠️ **Bunun bir sonucu daha var:** araç düz anahtarı **kendi süreci içinde** üretip
kullanmak zorunda (çipe yazmak için lazım), yani `sun.Wrap` ile sarmalayıp
`sun.Zero` ile silmek **aynı süreçte** olmalı. Anahtarın süreç dışına çıktığı her
tasarım, aşağıdaki "Anahtar hijyeni"ni ihlal eder.

### Araç yolu — KARAR DEĞİL, KARAR ÖNERİSİ (FAZ B, ve kararı kullanıcı verir)

> ✅ **BU BAŞLIK VE ALTINDAKİ "SEÇİLMEDİ" CÜMLESİ → ADR 0017 İLE KAPANDI
> (2026-08-20).** Kullanıcı **B yolunu** (kendi Android uygulamamız, APDU rölesi)
> seçti. Aşağısı tarihsel kayıttır, **silinmedi**; bugünkü karar
> [ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)'dedir.

> 🔴 **BU BAŞLIK ALTINDA HİÇBİR ŞEY SEÇİLMEDİ.** Yeni bir taşıma bağımlılığı
> CLAUDE.md §1 gereği **kullanıcı onayı** ister; bir ajan onu kendi başına
> seçemez. Burada yazılı olan şey **seçenekler ve bilinme dereceleri**dir.
> *(→ ADR 0017 ile kapandı, 2026-08-20.)*

Yukarıdaki kısıt (canlı oturum zorunlu) iki şekle izin veriyor:

| Yol | Şekli | Bugünkü bilgi derecesi |
|---|---|---|
| **A — kendi yazıcımız** | Go içinde PC/SC benzeri bir okuyucu katmanı + `AuthenticateEV2First` / `ChangeFileSettings` / `ChangeKey` akışının kendi uygulamamız. Anahtar süreçten hiç çıkmaz, `sun.Wrap`/`sun.Zero` aynı süreçte. | **Kısıtı biliyoruz, maliyeti bilmiyoruz.** APDU akışının şartları AN12196'dan **birebir** okundu (yukarıdaki tablo). Okuyucu donanımı, sürücü katmanı ve yeni bağımlılık **ölçülmedi**. |
| **B — üçüncü parti yazıcı + ayrı yükleyici** | Encode'u hazır bir araçla yap, satırı ayrı bir filtreyle DB'ye yükle. | **En zayıf halka burada: anahtar araçtan çıkar.** Araç düz anahtarı üretiyor/gösteriyorsa "Anahtar hijyeni" maddesi 1 **ihlal edilir**; ihlal etmeyen bir araç bulunmadan bu yol yoldur denemez. |

⚠️ **Aşağıdaki araç iddiaları ZAYIF KANIT'TI — ölçüm değil, okumaydı.** Bunlar
arama sonucu özetlerinden derlenmişti; ilgili sayfaların bir kısmı **403**
döndürdüğü için birincil kaynaktan doğrulanamamıştı. AN12196 atıflarıyla **aynı
sınıfta değildi** ve öyle okunmamalıydı:

- **NXP TagWriter** — anahtar değiştirmediği, yani `ChangeKey` yapmadığı
  *okundu*; aynı okumada bu iş için RFIDDiscover + PEGODA okuyucu işaret ediliyor.
  **O gün doğrulanamadı.**
- **TagXplorer** — artık desteklenmediği *okundu*. **O gün doğrulanamadı.**
- **Maliyetler hiç ölçülmedi.** Ne okuyucu fiyatı, ne lisans, ne teslim süresi.
  *(O gün buraya şu da yazılmıştı: "Bu bölüm bir maliyet karşılaştırması
  **taşımıyor**; taşıdığını iddia eden bir cümle görürsen o cümle yanlıştır."
  🔴 **O cümle bugün geçersizdir** — aşağıdaki kutuya bak.)*

> ✅ **ÜÇÜ DE 2026-08-20'DE ÖLÇÜLDÜ — VE ÜÇÜNCÜSÜNÜN SONUCU YUKARIDAKİ İKİ
> ŞEKLİ DE DEĞİŞTİRDİ.** Yukarıdaki üç madde **silinmedi**: bir sonraki okur
> *"zayıf iddia"* etiketiyle *"ölçülmüş sonuç"*u karıştırmasın diye tarihsel
> kayıt yerinde duruyor. **Bugünkü hâl** ve kaynakları bir alt bölümdedir:
> **"Dört yol — ölçüldü"**. 🔴 Bu paragrafın *"maliyet karşılaştırması
> taşımıyor"* cümlesi **artık yanlıştır**; bölüm bir maliyet karşılaştırması
> **taşıyor** ve her satırı tarihli. ⚠️ Aynı bayat cümle
> [m8-deploy-pilot.md](../docs/plan/m8-deploy-pilot.md) → M8-05'te de duruyor
> (FAZ A kriter 2 satırı ve *"maliyet hâlâ ölçülmedi"* cümlesi); o kart bu turda
> **kapsam dışıydı**, düzeltilmedi — çelişki görürsen **bu dosya** günceldir.

**Karar için gereken ölçüm (FAZ B'nin ilk adımı):** eldeki 10'luk NTAG 424 DNA
paketiyle, seçilen okuyucu üzerinde **tek bir çipi** kişiselleştirmek — ve bunu
yaparken anahtarın süreç dışına **çıkmadığını** göstermek. Aşağıdaki bölüm bu
ölçümün **önündeki** soruları (yapabilir mi · anahtar nerede · ne yazacağız · ne
kadar) kapatır; **çipe dokunan** yarı hâlâ FAZ B'dedir.

### Dört yol — ÖLÇÜLDÜ (2026-08-20). KARAR YİNE VERİLMEDİ.

> ✅ **BAŞLIĞIN İKİNCİ CÜMLESİ → ADR 0017 İLE KAPANDI (2026-08-20, aynı gün
> akşamı).** Ölçüm geçerli ve değişmedi; **seçim yapıldı: B yolu.**
> [ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md).

> 🔴 **BU ALT BÖLÜM YUKARIDAKİ "İKİ ŞEKİL" TABLOSUNUN YERİNE GEÇMEZ — ONU
> AÇAR, VE EŞLEMESİ BURADA YAZILI** (iki tablonun sessizce ayrışması bu dosyanın
> bilinen hastalığıdır):
>
> | Yukarıdaki şekil | Aşağıdaki yol |
> |---|---|
> | **A — kendi yazıcımız** | **üç taşımaya ayrılıyor**: **A** (USB okuyucu) · **B** (Android) · **C** (iOS) |
> | **B — üçüncü parti yazıcı + ayrı yükleyici** | **D** |
>
> İkisi çelişirse **aşağıdaki** geçerlidir: bu ölçüldü, yukarıdaki okunmuştu.
>
> ⚠️ **BU BÖLÜM BAYATLAR VE TARİHİ ONUN İÇİN YAZILI.** Her fiyat, her sürüm
> numarası, her mağaza satırı **2026-08-20**'de görülen hâldir. Fiyatlar ve
> uygulama özellikleri altı ayda değişir; **API ve entitlement kuralları** daha
> yavaş ama onlar da değişir (aşağıda iOS 26.4'ün taze bir örneği var). Altı
> aydan eski okuyorsan **yeniden ölç**.
>
> 🔴 **VE BURADA HÂLÂ HİÇBİR ŞEY SEÇİLMEDİ.** Dördü de en az bir yeni bağımlılık
> ya da yeni bir dil/araç zinciri getiriyor → **CLAUDE.md §1 gereği seçim
> kullanıcınındır.** Aşağıdaki metin okumaları sayılarla yan yana koyar,
> aralarından birini **işaretlemez**.
> *(→ ADR 0017 ile kapandı, 2026-08-20: kullanıcı **B**'yi seçti. Aşağıdaki
> karşılaştırma tarihsel kayıt olarak duruyor ve hâlâ doğrudur.)*

#### Önce üç iddia kapandı — ikisi ÇÜRÜDÜ

**1. 🔴 "Hazır uygulamalar `ChangeKey` yapamaz" — BLOKET HÂLİYLE YANLIŞ.**
Bu oturumun başında öne sürülen ve *doğrulanamayan* iddia buydu. Ölçüldü:
**dar hâli doğru, geniş hâli yanlış.**

- ✅ **Doğru olan yarı, ve artık ÜRETİCİNİN KENDİ AĞZINDAN:** wakdev'in
  **NFC Tools**'u NTAG 424 DNA'da yalnız *"teknik bilgi okuma, NDEF kaydı yazma
  ve silme"* yapar; kriptografik yapılandırma için *"özel bir okuyucu ve sunucu
  tarafı altyapı"* gerektiğini **kendi bilgi tabanında** yazıyor.
  ([wakdev KB → NXP NTAG424 DNA](https://www.wakdev.com/en/knowledge-base/nfc-chips/nxp-ntag424-dna.html),
  2026-08-20'de okundu.) Aynı sayfa sık yapılan hatayı da kapatıyor: NTAG 424 DNA
  **NTAG21x'in 32-bit parola mekanizmasını kullanmaz** — koruma tamamen AES-128
  anahtarlarındadır. *"NFC Tools tag'e parola koyabiliyor"* diyen her cümle bu
  iki mekanizmayı karıştırıyor.
- ✅ **NXP TagWriter ve TagInfo da yapamıyor** — ve bu artık okuma değil ölçüm:
  iki uygulamanın Play Store açıklamalarında `424`, `SDM`, `SUN`, `AES`,
  `ChangeKey`, `personaliz` dizeleri **sıfır kez** geçiyor. TagWriter'ın tek
  güvenlik özelliği yine **NTAG21x** parolasıdır ve NTAG 424 DNA desteklenen çip
  listesinde bile yok.
- ❌ **ÇÜRÜYEN YARI — istisna VAR, hem de birden çok, hem de telefonda:**
  - **NFC.cool Tools** (iPhone **ve** Android, okuyucu gerektirmez) kendi blog
    sayfasında *"herhangi bir slotu değiştir, fabrika ayarına döndür, ya da başka
    bir cihazda kurduğun bir anahtarı gir"* ve *"SUN'ı aç, tag her tap'i
    imzalamaya başlasın"* diyor; UID'nin açık mı şifreli mi mirror'lanacağını da
    ayarlatıyor. Yani **`ChangeKey` + `ChangeFileSettings`, telefondan.**
    ([nfc.cool blog, sayfa tarihi 2026-07-24](https://nfc.cool/blog/ntag-424-dna-counterfeit-proof-nfc-tags/) ·
    [App Store listesi](https://apps.apple.com/us/app/nfc-cool-tools-tag-reader/id1249686798))
  - **NFC Developer App** (Arx Research; iOS + Android) NTAG 424 DNA'yı dinamik
    URL özelliğiyle programlıyor; kendi demo sayfası kullanıcıya *"master
    anahtarını oluştur"* dedirtip **16 baytı elle uygulamaya girdiriyor**.
    ([nfc.dev/n424/demo](https://nfc.dev/n424/demo/), 2026-08-20)
  - **GoToTags Desktop** (Windows/macOS/Linux + PC/SC okuyucu) *"File Access
    Rights"* ve *"SDM Settings"* yazıyor.
- 🔴 **AMA HİÇBİRİ BİZİM SORUNUMUZU ÇÖZMÜYOR, VE SEBEBİ ÜRÜN KUSURU DEĞİL —
  PROTOKOL:** `ChangeKey` (0xC4) gövdesi `EskiAnahtar ⊕ YeniAnahtar ‖ KeyVer ‖
  CRC32(YeniAnahtar)` olarak, oturum anahtarı altında şifreli gider. **Bu APDU'yu
  kuran şey düz anahtarı görmek ZORUNDADIR.** Anahtarı hiç görmeyen bir tüketici
  ürünü yoktur ve olamaz. Kaynakta teyit: `johnnyb/ntag424-java` →
  `ChangeKey.run(comm, keyNum, byte[] oldKey, byte[] newKey, version)`; NXP'nin
  kendi **TapLinx** API'si → `changeKey(int keyNo, KeyType, byte[] oldKey,
  byte[] newKey, byte keyVersion)`. **İki imza da düz anahtarı parametre olarak
  alıyor.**

  Yani D yolunun tek gerçek sorusu *"araç yapabiliyor mu"* değil,
  **"düz anahtar nereye düşüyor"**dur — ve cevap her seferinde *"aracın içine"*:
  NFC.cool anahtarı bir **parola cümlesinden türetip telefonun keychain'ine**
  yazıyor (⚠️ türetme fonksiyonu **belgelenmemiş** — `internal/sun`'ın aynı 16
  baytı bağımsız üretmesi imkânsız, yani `aes_key_ref`'e yazacak değeri
  **bilemeyiz**); NFC Developer App anahtarı **elle girdiriyor**.

**2. ✅ WebNFC ham APDU VERMİYOR — kapandı, ve kalıcı olarak.** Tarayıcıdan
yapılabilseydi §1'e hiçbir şey eklemeyecektik; yapılamıyor. W3C şartnamesi
(⚠️ adres **taşındı**: `w3c-cg.github.io/web-nfc`) birebir şöyle diyor:
*"The current scope of this specification is NDEF. Low-level I/O operations
(e.g. ISO-DEP, NFC-A/B, NFC-F) and Host-based Card Emulation (HCE) are not
supported within the current scope."* Chrome'un belgesi gerekçeyi ekliyor:
*"Web NFC is limited to NDEF because the security properties of reading and
writing NDEF data are more easily quantifiable."*
([w3c-cg.github.io/web-nfc](https://w3c-cg.github.io/web-nfc/) ·
[developer.chrome.com/docs/capabilities/nfc](https://developer.chrome.com/docs/capabilities/nfc))
İlgili iki talep **on yıldır açık ve hiç uygulanmadı**: `w3c/web-nfc` **#101**
("Support ISODep", 2016-02-11 açıldı, son yorum 2024-09-23) ve **#578**
("APDU transmit", 2020-05-23). Şartname editörü #101'de: *"this API does not
intend to provide a low level access… with ISODep etc we open a point-blank
Pandora box."* **Kalıcı olarak yok say.** (Ayrıca WebNFC yalnız Android/Chrome
89+; iOS'ta hiç yok.)

**3. 🔴 "NXP'nin resmî yolu şu araçtır" ÖNERMESİ DE YANLIŞMIŞ — AN12196 ARAÇ
VARSAYMIYOR.** Yukarıdaki zayıf-kanıt bloğu RFIDDiscover + PEGODA'yı *"bu iş
için işaret edilen"* araç diye anıyordu. Belge okundu (rev. 1.8, 2020-11-17 **ve**
rev. 2.0, 2025-03-04): **kişiselleştirme bölümü hiçbir donanım ya da yazılım adı
vermiyor.** Ham APDU dizisidir — 17 numaralı adım, `ChangeKey`'le biter, ve
kendi ifadesiyle *"Following steps are optional and used as an example only."*
Her iki revizyonda **"PEGODA" ve "TagXplorer" sıfır kez** geçiyor. Tek araç
listesi ayrı bir bölümdedir (rev. 1.8 §10.1 / rev. 2.0 §9.1) ve orada
**RFIDDiscover · NXPRdLib · TapLinx · TagInfo · TagWriter** sayılır — TagWriter'ın
tarifi de zaten *"configure, write **NDEF data**"*.
⚠️ **nxp.com JavaScript çalıştırmayan istemcilere her PDF'i 404 döndürüyor**
(anti-bot; gerçek bir 404 değil) — önceki turun "403/doğrulanamadı"sının sebebi
büyük olasılıkla budur. Çalışan yollar: bir satıcı aynası
([rev. 1.8 PDF](https://cdn.webshopapp.com/shops/19172/files/428229523/ntag424-an12196.pdf))
ve archive.org anlık görüntüsü (rev. 2.0).
**Sonuç: AN12196 bilinçli olarak araç-bağımsızdır** — yani "resmî araç" diye bir
kısıtımız yok, dört yol da belgeye eşit uzaklıkta.

Mobil tarafta NXP'nin işaret ettiği tek şey **TapLinx**'tir (bir uygulama değil,
Android **kütüphanesi**) ve **yaşıyor**: v5.0.0, 2025-11-03; Android 4.1–15;
*"generate APDUs… leveraging the Android SDK's transceive functionality"*
(RN00263 rev. 3.0, 2025-11-13). ⚠️ **Lisans kapılı**: mifare.net üzerinde paket
adına bağlı uygulama kaydı + `registerJavaApp()` ile yüklenen bir lisans dosyası
ister. Bedelsiz ama **hesaba bağlı**, ve bir bağımlılıktan fazlası: bir hesap
ilişkisi.

#### 🔴 APDU RÖLESİ — dört yolun GERÇEK ayırıcısı burasıdır

Bu bölümün üstünde yazılı olan kısıt (*"offline script üret, sonra yapıştır"
mümkün değil; `RndB` ve `TI` çipten gelir*) **değişmedi ve doğru**. Ama ondan
**ikinci** bir sonuç çıkıyor ve karar tam olarak orada duruyor:

> **Çipe dokunan şeyin kriptografiyi yapması GEREKMİYOR.** `AuthenticateEV2First`
> → `ChangeFileSettings` → `ChangeKey` akışında dokunan taraf yalnız **bayt
> taşır**: C-APDU'yu çipe verir, R-APDU'yu geri alır. Şifre çözme, oturum
> anahtarı türetme, `Old ⊕ New ‖ KeyVer ‖ CRC32` gövdesini kurma ve yanıt MAC'ini
> doğrulama — hepsi **saf hesaptır** ve nerede koştuğu çipin umurunda değildir.

Akış rölede şöyle bölünür (numaralar AN12196'nın kişiselleştirme adımlarıdır):

| Kim | Ne yapar |
|---|---|
| **Taşıyıcı** (okuyucu / telefon) | `9071…` gönderir → `E(K, RndB)` alır → **sunucuya iletir** → dönen 32 baytı `90AF…` ile gönderir → yanıtı iletir → sunucunun kurduğu `905F…` ve `90C4…` APDU'larını sırayla gönderir, yanıtları iletir |
| **Sunucu (Go)** | `crypto/rand` ile 16 baytı üretir · `E(K,RndB)`'yi çözer · `RndA‖RndB'`'yi kurar · `TI` + `KSesAuthENC` + `KSesAuthMAC` türetir · `CmdCtr`'ı sayar · iki komut gövdesini şifreler ve MAC'ler · yanıt MAC'lerini doğrular · sonunda `sun.Wrap` + `sun.Zero` |

**Bunun üç sonucu var ve üçü de karar ağırlıklı:**

1. 🔴 **Düz anahtar sunucudan hiç çıkmaz.** ADR 0003 md. 5'in *"düz anahtar bu
   adımdan sonra hiçbir yerde kalıcılaşmaz"* şartı ve *"Anahtar hijyeni"* maddesi
   1 (`Wrap`/`Zero` **aynı süreçte**) **ihlal edilmeden** sağlanır — hatta bu
   bölümün başındaki *"araç düz anahtarı kendi süreci içinde üretmek zorunda"*
   cümlesinden **daha dar** bir garantidir: süreç artık bir operatör makinesinde
   değil, sunucudadır. **ADR tadili gerekmez.**
2. ✅ **Telefon/okuyucu tarafı APTAL bir borudur.** Kripto orada koşmayınca A, B,
   C yollarının *"bizde ne yazılacak"* maliyeti çöker: kalan iş bir APDU
   döngüsü + HTTPS çağrısıdır (mertebe: **birkaç yüz satır**). Özellikle iOS'ta
   bu, aşağıda sayılan **CryptoKit'te AES-CBC ve AES-CMAC olmaması** engelini
   **tamamen ortadan kaldırır** — ki bilinen tek açık iOS denemesinin tam olarak
   takıldığı yer orasıydı.
3. ⚠️ **Bedeli sunucuda YENİ ve DURUMLU bir makinedir.** Bir encode oturumu
   `RndA`, `TI`, `KSesAuthENC`, `KSesAuthMAC`, `CmdCtr` taşır ve **6–17 HTTP
   gidiş-dönüşü** boyunca yaşamak zorundadır. Bugün böyle bir şey yok. Bu, `tap`
   yolunun durumsuz şeklinden farklı bir hayvandır ve **kendi kabul kriterlerini
   ister** (oturum ömrü, eşzamanlılık, iptal, yarım kalan encode'un `tags`
   satırına ne yaptığı). ⚠️ **Ölçmedim:** böyle bir oturumun uçtan uca gecikmesi
   ve hata modları **denenmedi** — protokol tarafı mümkün diyor, gerçek ölçüm
   FAZ B'dedir.

**Protokol tarafı bunu neden engellemiyor:** ISO/IEC 14443-4'te `FWT`
(frame waiting time) **PICC'in yanıt gecikmesini** sınırlar, PCD'nin iki komut
arasındaki düşünme süresini **değil**. Yani araya bir ağ turu koymak standardın
zaman bütçesine dokunmaz. 🔴 **Tek gerçek zaman duvarı iOS'tadır** ve aşağıda C
yolunda sayılıdır (~20 sn, sert).

**D yolunda röle MÜMKÜN DEĞİLDİR** ve sebep yukarıda: `ChangeKey` gövdesini kuran
taraf düz anahtarı görmek zorunda; üçüncü parti araç o gövdeyi kendi kuruyor.
Röle, aracın **bizim** olmasını gerektirir.

#### Dört yol — karşılaştırma

| | **A · USB okuyucu + kendi Go aracımız** | **B · kendi Android uygulamamız** | **C · kendi iOS uygulamamız** | **D · üçüncü parti araç + kendi yükleyicimiz** |
|---|---|---|---|---|
| **424 DNA personalizasyonunu yapabilir mi?** | ✅ **Evet.** ACR1252U belgesi: ISO 14443-4 uyumlu her kart ISO 7816-4 APDU anlar, *"ACR1252U will handle the ISO 14443 Parts 1-4 Protocols internally"* → düz `SCardTransmit` yeter, satıcıya özel escape komutu **gerekmez** | ✅ **Evet.** `IsoDep.transceive()` belgede birebir *"Send **raw** ISO-DEP data to the tag"*; NDEF kısıtı **yok**, `CLA=0x90` ailesi dokunulmadan geçer | ✅ **Evet, ve Apple'ın belgelediği YOL VAR** — ama şekli seçmek gerekiyor (aşağıda) | ✅ Evet (NFC.cool · NFC Developer App · GoToTags) |
| **Ham APDU sınırı** | Kısa **ve** genişletilmiş APDU (CCID `dwFeatures 0x000404BA`, `dwMaxCCIDMessageLength 522`) | `getMaxTransceiveLength()` ile sorulur; genişletilmiş için `isExtendedLengthApduSupported()`. **Bizde önemsiz** — 424 DNA'nın her komutu 255 baytın altında | Aynı sebeple önemsiz | — |
| 🔴 **Düz anahtar nerede durur?** | **Sunucuda** (röle) **veya** operatör makinesindeki süreçte. İkisi de ADR 0003 md. 5 ile uyumlu | **Sunucuda** (röle). Telefonda **hiç** durmaz | **Sunucuda** (röle). Telefonda **hiç** durmaz | 🔴 **ARACIN İÇİNDE** — kaçınılmaz (protokol gereği). NFC.cool'da telefon keychain'inde, belgelenmemiş bir türetmeyle; NFC Developer App'te elle girilir |
| **Bizde ne yazılacak** | 2 bileşen: PC/SC taşıması + DESFire EV2 güvenli mesajlaşma (Go). Röle varsa taşıma ayrı bir ikili | 3 bileşen: Android APDU borusu (Kotlin/Java) + sunucuda encode oturumu + EV2 kripto (Go) | 3 bileşen: iOS APDU borusu (Swift) + sunucuda encode oturumu + EV2 kripto (Go) | 1 bileşen: `seedkeys` emsalinde saf bir yükleyici filtresi (Go) — **ama** yüklenecek anahtarı **araçtan** almak gerekir |
| **Yeni bağımlılık (§1)** | **1 Go modülü** (PC/SC bağlaması) — ya da kendi bağlamamızı yazarsak 0 modül + CGO. EV2 kripto `crypto/aes` + `crypto/cipher` ile stdlib'de kalır | **Yeni bir dil ve derleme zinciri**: Kotlin/Java + Android SDK + Gradle. Go tarafında **0 yeni modül** (röle sayesinde). Node **gerekmez** | **Yeni bir dil ve derleme zinciri**: Swift + Xcode + **macOS derleme makinesi**. Go tarafında **0 yeni modül** | Go tarafında **0 yeni modül**. Ama bir **satıcı bağımlılığı** ve bir **abonelik** |
| **Dağıtım sürtünmesi** | Yok — operatör makinesinde koşan bir CLI | APK **yan yükleme yeter**, mağaza hesabı gerekmez. ⚠️ 2027 kapısı aşağıda | 🔴 **Ücretli Apple Developer Program ZORUNLU** — bedava sağlama profili NFC entitlement'ı **vermez** | Yok |
| **Maliyet (2026-08-20)** | Okuyucu **~€42–48** (ACR1252U, AB satıcısı) · yazılım €0 | €0 (yan yükleme) · cihaz elde | **$99/yıl** + macOS makinesi · cihaz elde | NFC.cool **$29.99/yıl** (ya da $3.99/ay) · GoToTags **~$0.05/tag, $25 asgari** · NFC Developer App: ücretsiz sürüm CAPTCHA'lı |
| **Tedarik (Malta)** | ShopNFC (IT) Malta'yı **adıyla** sayıyor: ücretsiz 7–14 iş günü ya da **€9.90'dan 3–5 iş günü**. AB içi → gümrük yok | — | — | — |
| 🔴 **ADR 0003 md. 5 / §4.7 etkisi** | ✅ İhlal yok | ✅ İhlal yok | ✅ İhlal yok | 🔴 **İHLAL** — düz anahtar süreçten çıkar, üçüncü bir yazılıma ve o cihazın kalıcı deposuna girer. Bu yolu seçmek **ADR tadili** demektir ve o **ayrı bir karardır** |
| **En büyük tek risk** | Go PC/SC bağlamalarının hiçbiri hem bakımlı hem CGO'suz hem çapraz platform değil (aşağıda) | Google'ın 2027 doğrulama kapısı | ~20 sn'lik **sert** bağlantı sınırı + iOS 26.4 API değişimi | Anahtar hijyeni **ve** NFC.cool'un belgelenmemiş anahtar türetmesi → `aes_key_ref`'e ne yazacağımızı bilemeyiz |

#### A — USB okuyucu + kendi Go aracımız · ayrıntı

**Okuyucu seçimi ölçüldü, ve iki popüler seçenek ELENDİ.** Kanıt CCID
sürücüsünün kendi cihaz veritabanıdır
([LudovicRousseau/CCID](https://github.com/LudovicRousseau/CCID),
[desteklenmeyenler listesi](https://ccid.apdu.fr/ccid/unsupported.html)):

| Okuyucu | APDU seviyesi | Durum |
|---|---|---|
| **ACR1252U** | kısa + genişletilmiş | ✅ önerilebilir |
| **ACR1552U-M1** | kısa + genişletilmiş | ✅ ACS'in kendi ACR122U halefi, USB-C var |
| **uTrust 3700 F** | kısa + genişletilmiş (1034 bayt) | ✅ en geniş, ⚠️ **AB fiyatı doğrulanamadı** |
| 🔴 **ACR122U** | **yalnız kısa** | ❌ **ELENDİ** |
| 🔴 **ACR1281U-C1** | kısa + genişletilmiş | ❌ **ELENDİ** |

🔴 **ACR122U'yu eleyen şey APDU uzunluğu DEĞİL — WTX.** Desteklenmeyenler
listesi birebir: *"**Time extension requests are not supported.**"* Oysa
`AuthenticateEV2First` ve `ChangeKey` çipe AES işi yaptırır ve çip **bekleme
süresi uzatması** ister; onu host'a iletmeyen okuyucu el sıkışmanın **ortasında**
zaman aşımına düşer. Aynı cümle **ACR1281U-C1** için de yazılı. Üstüne ACS
ACR122U'yu **kullanımdan kaldırdı** ve piyasadaki
[sahteler için resmî bir bildiri](https://www.acs.com.hk/en/press-release/2266/)
yayımladı. ⚠️ **PN532 kartları da yol değil** — CCID cihazı değiller, PC/SC yolu
hiç yok.

⚠️ **DigiKey / Mouser / RS / Farnell'den ISMARLAMA:** oralarda "ACR1252U" adıyla
**~€152**'ye listelenen şey **Carlo Gavazzi**'nin bir röle aksesuarıdır — aynı
parça numarası, başka ürün. AB satıcı örnekleri (2026-08-20, KDV hariç/dahil
karışık): Kartenstudio (DE) **€41.50 + KDV** · Identible (DE) **€42.00 net** ·
ShopNFC (IT) **€48.30** · MEGATEH (EE) ACR1552U-M1 **€46.40**.
🔴 **Birleşik Krallık satıcılarından alma** — Malta'ya üçüncü ülke ithalatıdır
(18% ithalat KDV + gümrükleme), ve €150 muafiyet eşiği **2026'da kalkıyor**
([EC, 2025-11-13](https://taxation-customs.ec.europa.eu/news/e-commerce-150-eur-customs-duty-exemption-threshold-be-removed-2026-2025-11-13_en)).

**Go tarafı — ve §1'in "tek statik binary"si burada gerçekten zorlanıyor:**

| Kütüphane | ★ | Son commit | Lisans | CGO? | Çalışma anı ihtiyacı |
|---|---|---|---|---|---|
| `ebfe/scard` | 102 | 2024-12-14 | BSD-2 | **Evet** (Windows hariç) | `libpcsclite` / `PCSC.framework` |
| `ElMostafaIdrassi/goscard` | 8 | 2025-01-25 | Apache-2.0 | **Hayır** (purego, `dlopen`) | aynı kütüphaneler, **çalışma anında** |
| `gballet/go-libpcsclite` | 5 | 2025-09-18 | BSD-3 | **Hayır** | **hiçbiri** — doğrudan `pcscd` soketi |
| `sf1/go-card` | 41 | 2022-08-12 | MIT | Evet | 🔴 **ARŞİVLENMİŞ** |

🔴 **"CGO yok" ile "native bağımlılık yok" AYNI ŞEY DEĞİL.** `goscard` CGO'suz
ama yine `libpcsclite`'ı çalışma anında arar. Bağımlılığı **tamamen** kaldıran
tek seçenek `go-libpcsclite` ve o da **kullanılamaz**: `doc_darwin.go` ile
`doc_windows.go` soket yolunu **boş dize** tanımlıyor (macOS için
[issue #3, 2019'dan beri açık](https://github.com/gballet/go-libpcsclite/issues/3);
Windows için #5, 2020) — **bizim geliştirme makinemiz darwin**. Ayrıca API'si
beş fonksiyon (`SCardControl` ve transaction yok) ve pcsc-lite'ın
**sürümlenmemiş, dahili** istemci/daemon ABI'sini yeniden uyguluyor.

⚠️ **Bir kaçış yolu var ve sayılmalı:** encode aracı **sunucu değildir**. §1'in
*"tek binary, statik deploy"* hedefi **sevk edilen ikiliye** aittir; ayrı bir
operatör CLI'si (`cmd/…`) CGO'yu **rahatça** göze alabilir ve o zaman CGO'suzluk
sorusu **karar taşımaz hâle gelir**. Bu bir karar değil, seçeneğin kendisidir.

**Hazır Go kütüphanesi YOK — bu bir bağımlılık seçimi değil, bir yazma işidir.**
`pkg.go.dev` `ntag424` için **tek** sonuç veriyor ve onu **sıfır** paket import
ediyor. Ölçülen iki aday da düşüyor:
`barnettlynn/nfctools` **tam** (SDM dahil) ama **LİSANSSIZ** (yani her hakkı saklı)
ve düz AES anahtarlarını diskteki `.hex` dosyalarından okuyor — §4.7 ile doğrudan
çarpışıyor · `dumacp/smartcard` (33★, Apache-2.0, testli) DESFire EV2 oturum
kurulumunu **doğru** yapıyor (`sv1`/`sv2` sabitleri, `TI`+`CmdCtr` IV'i) ama
**`SDM` deposunda sıfır kez geçiyor** — yani `ChangeFileSettings`'in tam da bizim
ihtiyacımız olan yarısı (SDM seçenek baytı, erişim hakları, üç baytlık
little-endian mirror ofsetleri) **eksik**. Port kaynağı olarak en temizi
[codeberg.org/jannschu/ntag424](https://codeberg.org/jannschu/ntag424) (Rust,
MIT/Apache-2.0, 2026-05-26) ve `johnnyb/ntag424-java` (47★, MIT, ⚠️ 2024-06-10'dan
beri commit yok).

#### B — kendi Android uygulamamız · ayrıntı

**Ham APDU: belgeyle kanıtlı.** Android'in `IsoDep` sınıfı birebir *"Provides
access to ISO-DEP (ISO 14443-4) properties and I/O operations on a Tag.
Applications must implement their own protocol stack on top of `transceive()`"*
diyor; `transceive(byte[])` ise *"Send **raw** ISO-DEP data to the tag and
receive the response"*. **NDEF kısıtı yoktur.** Gereken izin sıradan
`android.permission.NFC`'dir (çalışma anı izni değil). Sınırlar:
`getMaxTransceiveLength()` bayt tavanını verir, `isExtendedLengthApduSupported()`
genişletilmiş APDU desteğini söyler (kısa APDU tavanı 255 yük / 261 çerçeve) ve
`setTimeout(int)` **transceive başına** ayarlanır. 🔴 **Android'de oturum ömrü
sınırı YOKTUR** — bağlantı çip alandan çıkana ya da `close()` çağrılana kadar
yaşar. Röle için bu, iOS'a göre **belirleyici bir üstünlüktür**.

**Dağıtım bugün: APK yan yükleme yeter.** Mağaza hesabı, inceleme, ücret yok.

⚠️ **AMA BİR TARİH VAR VE KARARA GİRMELİ.** Google, sertifikalı Android
cihazlarda kurulacak uygulamalar için **geliştirici doğrulaması** getiriyor:
geliştirici API'leri ve "güç kullanıcı" akışı **2026 Ağustos**, dört ülkede
mağaza son tarihi **2026-09-30**, **küresel yayılım 2027**. Doğrulama KYC kimlik
kontrolü + paket adı ve imza anahtarı kaydı istiyor.
([developer.android.com/developer-verification](https://developer.android.com/developer-verification),
2026-08-20) İki hafifletici var ve ikisi de bizim ölçeğimize **uyuyor**:
**ADB ile kurulum** ve öğrenci/hobici için **"limited distribution" hesabı —
20 cihaza kadar, kimlik belgesi ve ücret istemeden**. Encode aracı **tek bir
operatör telefonunda** koşacaksa bu kapı bugün de 2027'de de kapanmıyor;
yine de **bilerek** girilmeli.

#### C — kendi iOS uygulamamız · ayrıntı

**API ve sürüm.** `NFCTagReaderSession` ve `NFCISO7816Tag.sendCommand(apdu:)`
**iOS 13.0+**. ⚠️ **iOS 26.4** `NFCTagReaderSession.init(pollingOption:delegate:queue:)`
başlatıcısını **kullanımdan kaldırdı**; yerine `init(configuration:delegate:queue:)`
geldi. Yazılacak kod ilk günden yeni başlatıcıya + geriye dönük dala göre
kurulmalı.

**Entitlement:** `com.apple.developer.nfc.readersession.formats`. Apple'ın
bugünkü belgesi **tek** değer sayıyor: **`TAG`** — *"Allows read and write access
to a tag using `NFCTagReaderSession`"*. Ayrıca `Info.plist`'te boş olmayan bir
`NFCReaderUsageDescription` **zorunlu**; entitlement eksikse oturum
`NFCReaderErrorSecurityViolation` ile başlamaz.

🔴 **AID listesi sorusunun cevabı beklenenden İYİ, çünkü İKİ ŞEKİL var:**

- **Şekil 1 — `NFCISO7816Tag`:** `Info.plist`'e
  `com.apple.developer.nfc.readersession.iso7816.select-identifiers` altında
  **`D2760000850101`** yazılır. Apple: sistem listedeki her kimlik için **kendisi
  `SELECT` gönderir** ve ilk başarılıda tag'i teslim eder. **Bu bizim için tam
  isabet:** NTAG 424 DNA'nın **tek** uygulaması vardır ve ISO 7816 DF adı zaten
  **odur** — yani "PICC seviyesine ayrı bir AID ile inmek" diye bir sorun **yok**,
  AN12196'nın bütün kişiselleştirme dizisi tam da o SELECT'in **arkasında**
  koşuyor. Apple'ın tek kısıtı `p1Parameter == 0x04` ile **kendi** göndereceğin
  `SELECT` içindir; `90 71 / 90 5F / 90 C4 / 90 AF` **`SELECT` değildir**, kısıta
  girmez.
- **Şekil 2 — `NFCMiFareTag`:** `D2760000850101`'i listeye **koymazsan** Apple
  aynı çipi MIFARE tag'i olarak verir ve `sendMiFareCommand` ile DESFire ailesine
  doğrudan komut gönderirsin — **`select-identifiers` hiç gerekmez**, `formats`
  entitlement'ı yeter. Apple'ın kendi belgesi bunu **DESFire için açıkça**
  tarif ediyor (*"To get the MIFARE DESFire tag as an `NFCMiFareTag` object,
  don't include `D2760000850101`"*), ve komut zincirlemeyi (`AF` parçaları)
  **kendisi birleştiriyor**.

🔴 **ASIL ENGEL AID DEĞİL, ZAMAN — VE SERT.** Bir Apple mühendisi geliştirici
forumunda (2025-10) birebir: *"The 20 second limit is a hard limit, and there is
no opportunities to extend it."* Yani oturum ~60 sn yaşasa da **çipe bağlı
kalınan süre ~20 saniyedir ve uzatılamaz**. Röle mimarisinde 17 APDU'nun her
biri bir NFC turu **artı** bir HTTPS turu demektir; kaba hesapla tur başına
~150 ms ile ~2.6 sn eder, yani **sığar** — ama TLS el sıkışması, yeniden deneme
ve zayıf şebeke için **marj dardır**. ⚠️ **ÖLÇMEDİM.** Bu tek sayı C yolunu tek
başına açar ya da kapatır ve **gerçek cihazla ölçülmelidir**.
([Apple Developer Forums 802895](https://developer.apple.com/forums/thread/802895))

**Dağıtım ve maliyet:** 🔴 **Ücretsiz kişisel sağlama profili NFC entitlement'ı
VERMEZ** — Apple'ın kendi "Supported capabilities (iOS)" tablosu *Near Field
Communication Tag Reading* satırını ücretsiz "Apple Developer" sütununda **✗**
işaretliyor. Yani **$99/yıl Apple Developer Program zorunludur**.
([developer.apple.com/help/account/reference/supported-capabilities-ios](https://developer.apple.com/help/account/reference/supported-capabilities-ios/))
İç ekipe dağıtım için **Ad Hoc** (kayıtlı cihazlara, **inceleme yok**) ya da
Apple'ın beta kanalının **internal** kolu (100 kullanıcıya kadar, App Review
yok) yeterlidir — ürünün adı bilerek yazılmadı, çünkü `Test` ile başlayıp büyük
harfle devam ettiği için `TestEveryNamedTestExists` onu **var olmayan bir teste
atıf** sayıyor (ölçüldü: 54 canlı / 53 bütçeli). Muafiyet listesi
(`cmd/tappa/testdata/known-dangling-citations.txt`) kendi başlığında *"yalnız
KÜÇÜLEBİLİR"* diyor, yani doğru çözüm ada dokunmamak;
**Enterprise Program ($299/yıl) uygun değil** — Apple ≥100 çalışan, D-U-N-S ve
doğrulama mülakatı şart koşuyor.
⚠️ Karıştırılan bir şey: bazı yazılar NFC'nin *"ayrı ticari sözleşme ve ek ücret"*
istediğini söylüyor — o, **HCE/güvenli eleman** (`…nfc.hce`) içindir. **Tag
okuma** $99'a dahil sıradan bir yetenektir.

**Röle olmadan iOS ayrıca kripto borcu getirir:** CryptoKit'te **AES-CBC de
AES-CMAC de yok**; bilinen tek açık iOS denemesi tam orada durmuş. **Röleyle bu
borç sıfırlanır** — telefon hiç kripto yapmaz.

#### D — üçüncü parti araç + kendi yükleyicimiz · ayrıntı

Teknik olarak **çalışır** ve bugün **hemen** yapılabilir; kırmızı çizgide durur.

- **Ne alınır:** NFC.cool Tools (**$29.99/yıl**, iPhone + Android, okuyucu
  gerektirmez) · GoToTags Desktop (**~$0.05/tag, $25 asgari**, PC/SC okuyucu
  ister) · NFC Developer App (ücretsiz sürüm CAPTCHA'lı).
- 🔴 **ADR 0003 md. 5 ve §4.7 ihlali — ve iki ayrı yerden:**
  1. Düz anahtar **bizim süreçlerimizin dışında** üretilir/girilir ve o cihazda
     kalıcılaşır. Bu bölümün *"Anahtar hijyeni"* maddesi 1 (`Wrap`+`Zero` **aynı
     süreçte**) **sağlanamaz**.
  2. NFC.cool'da anahtar bir **parola cümlesinden belgelenmemiş bir türetmeyle**
     üretilip keychain'e yazılıyor → `internal/sun` aynı 16 baytı **bağımsız
     üretemez**, yani `aes_key_ref`'e yazılacak değeri **bilemeyiz**. Ham hex
     girme yolu varsa o kullanılmalı — ama o zaman da (1) geçerli.
- ⚠️ **"Encode'lu tedarikçiden al" bunun daha kötü hâlidir ve Q10 onu zaten
  reddetti.** Ölçüm bunu doğruluyor: bu hizmeti veren satıcılar master sırrın
  kendi tesislerinden **çıkmadığını** açıkça yazıyor — yani üçüncü bir taraf
  duvardaki plaketlerimiz için **süresiz geçerli SUN URL'leri üretebilir**, yani
  **check-in uydurabilir**. Bu bir zahmet değil, **diskalifiye**dir.
- **Bu yolu seçmek bir ADR tadili demektir** ve bu **ayrı bir karardır** — bir
  araç seçimi olarak sessizce alınamaz.

#### Ölçemediklerim — tahmin yerine boşluk

1. **Hiçbir çip encode edilmedi.** Bu bölümün tamamı belge, satıcı sayfası ve
   API referansı okumasıdır. **Silikonla hiçbir şey doğrulanmadı** — FAZ B
   listesinin **dokuz** maddesi yerinde duruyor (2026-08-20'de sekizden dokuza
   çıktı: md. 9, anahtar 0 maruziyeti).
2. **iOS'un ~20 sn sınırı altında bir röle turunun gerçekten yetişip
   yetişmediği.** Kaba hesap yetiyor diyor; **ölçüm yok**. C yolunu tek başına
   açacak/kapatacak sayı budur.
3. **NTAG 424 DNA'nın kendi tarafında bir oturum zaman aşımı olup olmadığı.**
   AN12196'da bulamadım. ISO 14443-4 tarafı sorun çıkarmıyor ama **çip tarafı
   ölçülmedi** — ve **konu hâlâ açıktır**.
   ⚠️ **GEREKÇE DÜZELTİLDİ (2026-08-20).** Bu madde *"veri sayfasına birincil
   kaynaktan erişilemedi (nxp.com JS'siz istemcilere 404 döndürüyor)"* diyordu;
   **artık yanlış**. Ölçüldü (düz `curl`, iki ayrı ajan, aynı gün):
   `nxp.com/docs/en/data-sheet/NT4H2421Gx.pdf` → **HTTP 200 + gerçek 97 sayfalık
   PDF** (rev. 3.0, 2019-01-31). Belge **elde** ve ADR 0017 ondan on üç sayfaya
   atıf yapıyor. Yani açık olan şey **erişim değil**, veri sayfasında bir oturum
   zaman aşımı **aranıp bulunamamış olmasıdır** — bu iki farklı boşluktur ve
   ikincisini birincisiyle açıklamak yanlış yere bakmaya yol açar.
4. **uTrust 3700 F ve Omnikey 5022'nin AB fiyatları** — doğrulanamadı.
5. **Gerçek ACR122U'nun bugünkü AB fiyatı** — zaten elendiği için aranmadı.
6. **GoToTags'in düz anahtarı nerede tuttuğu** belgelenmemiş; D için **en kötü
   varsayım** alındı (araçta durur).
7. **community.nxp.com Cloudflare 403 veriyor ve Wayback anlık görüntüsü yok** —
   NXP forumundan gelen her iddia yalnız arama özeti düzeyindedir ve bu bölümde
   **karar taşıyan hiçbir cümle** oraya dayanmıyor.

#### 🔴 Kararı değiştirecek TEK ölçüm

Yukarıdakilerin arasında **bir tanesi** okumaları yer değiştirtir:
**iOS'ta ~20 saniyelik sert bağlantı sınırının altında tam bir röleli
kişiselleştirme turunun yetişip yetişmediği.** Yetişiyorsa C yolu B ile aynı
sınıfa çıkar ve *"donanım almadan, elimizdeki telefonla, anahtar sunucudan
çıkmadan"* şeklinde tek bir cevap belirir. Yetişmiyorsa C **düşer** ve seçim
**A ile B** arasına iner — biri **~€45 donanım + 1 Go bağımlılığı**, öteki
**€0 donanım + yeni bir dil zinciri ve 2027 doğrulama kapısı**.
Diğer altı boşluğun hiçbiri bu yer değiştirmeyi yapmaz.

### Encode ayarları — ADR 0003'ten NORMATİF türetilmiştir

Bu tablo bir tercih listesi değildir. Her satırın kaynağı yazılıdır; bir satırı
değiştirmek **ADR değişikliğidir**, ayar değişikliği değil.

> 🔴 **AŞAĞIDAKİ AN12196 ATIFLARI rev. 1.8 NUMARALANDIRMASIYLADIR; rev. 2.0
> karşılığı her satırda parantez içindedir.** Bu alt bölüm daha önce **altı** atıf
> taşıyıp **hiçbirine** revizyon yazmıyordu, ve o eksiklik masum değil: rev. 2.0
> (4 Mart 2025) §1 "Abbreviations"ı belgenin **sonuna** taşıdı, bu yüzden §2–§10
> arası her üst bölüm **bir aşağı kayıyor** (§4 → §3, §6 → §5). Yani revizyonsuz
> bir "§6.9" rev. 2.0'da **başka bir bölümü** gösterir — rev. 2.0'ın §6'sı
> "Personalization example" değil **"Special functionalities"**tir. Alt bölüm
> derinliği korunuyor ama **tablo numaraları korunmuyor**; ikisi ayrı ayrı
> aranmalıdır. Kuralın ölçülmüş tam hâli ve tablo eşleşmeleri:
> `internal/sun/an12196_kat_test.go` dosya başlığı.

| Ayar | Değer | Kaynak / neden |
|---|---|---|
| Mirroring modu | **plain** (şifreli PICC **değil**) | ADR 0003 md. 1 (Q05). UID zaten public; şifreli PICC **paylaşılan meta-read anahtarı** ister ve Q06 ile çelişir. |
| UID mirror | **açık**, 7 bayt → 14 hane | ADR 0003 md. 1. AN12196 rev. 1.8 §4.4.1 (rev. 2.0: §3.4.1): `UIDOffsetLength: 14`. |
| SDM read counter mirror | **açık**, 3 bayt → 6 hane | ADR 0003 md. 1. AN12196 rev. 1.8 §4.4.1 (rev. 2.0: §3.4.1): `SDMReadCtrOffsetLength: 6`. |
| **SDM MAC girdisi** | **BOŞ** — `SDMMACInputOffset == SDMMACOffset` | ADR 0003 md. 2. AN12196 rev. 1.8 §4.4.4.2.1 tablo 5 (rev. 2.0: §3.4.4.2.1 tablo **4**) bunu birebir tanımlıyor: *"SDMMAC = MACt(KSesSDMFileReadMAC; **zero length input**)"*. |
| SDM MAC | 8 bayt → 16 hane, URL'nin **sonunda** | ADR 0003 md. 1/6. AN12196 rev. 1.8 §4.4.1 (rev. 2.0: §3.4.1): *"CMAC shall be appended to the end of NDEF."* |
| Dosya okuma izni | **açık** (kimlik doğrulamasız okuma) | Tap akışı anonim bir tarayıcıdan gelir; okuma kapalıysa SUN hiç yayılmaz. AN12196 rev. 1.8 §6.9 tablo 19 (rev. 2.0: §5.9 tablo **18**) örneğinde `FileAR.Read = 0xE` (serbest). |
| Dosya **yazma** izni (`FileAR.Write`) | **anahtar `0x01`** | 🔴 **KARAR: [ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §6 md. 13 (2026-08-21, M8-05 FAZ B2b).** Bu hücre daha önce *"anahtarla kilitli — ama hangi anahtar KARARA BAĞLANMADI"* diyordu. Yazma serbest kalırsa duvardaki plaketin NDEF'ini herkes değiştirebilir. AN12196 örneğinin gösterdiği **anahtar 0** ALINMADI: uygulama anahtarı 0, kişiselleştirilene kadar (ADR 0017 §6 md. 5) **fabrika varsayılanıdır, yani halka açıktır** — yazmayı ona kilitlemek kapıyı kilitleyip anahtarı paspasın altına bırakmaktır ([ADR 0005](../docs/adr/0005-kabul-edilen-riskler.md) risk 8). `0x01` plaket-başına rastgeledir ve yalnız `tags.aes_key_ref` içinde sarmalı durur. ⚠️ **Tek başına yetmez ve o yüzden aşağıdaki satırla BİRLİKTE okunur** — tablo 9'a göre `FileAR.ReadWrite` de `WriteData`'ya kapı açar. Kod: `internal/sun/filesettings.go` → `TappaNDEFSettings`. |
| `K_SDMFileRead` | plaket-başına **rastgele** AES-128 | ADR 0003 md. 3 (Q06). `crypto/rand`; hiçbir şeyden türetilmez. |
| `K_SDMFileRead`'in anahtar NUMARASI | **`0x01`** | 🔴 **KARAR: [ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §5.0 karar 1 (2026-08-20).** Sıfırdan farklı olmak zorunda: anahtar 0 kart master anahtarıdır (veri sayfası §8.2.4.1) ve `ChangeKey` gövdesinin **şekli** numaraya bağlıdır (veri sayfası tablo 63: anahtar 0 → 17 bayt `NewKey ‖ KeyVer`; anahtar 1–4 → 21 bayt `(NewKey XOR OldKey) ‖ KeyVer ‖ CRC32NK`). `0x01`, AN12196 rev. 1.8 **§6** (rev. 2.0: **§5**) örnek yapılandırmasının `KSDMFileRead`'iyle aynı numaradır. |
| Fabrika varsayılan anahtarı | **değiştirilir** | ADR 0003 md. 5: *"Bu varsayılan anahtar üretimde **asla** kullanılmaz"*; `K_SDMFileRead` varsayılandan değiştirilir. |
| **Uygulama anahtarı `0x00` (AppMasterKey)** | **kişiselleştirilir, EN SON adım** | 🔴 **KARAR: ADR 0017 §5.0 karar 2 (2026-08-20) — bu hücre daha önce *"FAZ B'de karara bağlanır"* diyordu ve o cümle artık YANLIŞTIR.** Fabrika varsayılanında kalırsa halka açık bir anahtarla `WriteData` mümkün olur → NDEF URL'si değiştirilebilir → **oltalama** ([ADR 0005](../docs/adr/0005-kabul-edilen-riskler.md) risk 8). Veri sayfası tablo 8 ayak notu aynı şeyi emrediyor. Veri sayfası §8.2.4.2 ayrıca **beş anahtarın hepsini** öneriyor (*"highly recommended to change all 5 keys at personalization"*). ⚠️ **SEVKİYAT BİR ŞEMA KARARINA BAĞLI** (ikinci anahtar nerede saklanacak — ADR 0017 §6 md. 5; üç şık aşağıda *"FAZ B'ye devredilenler"* md. 9'da). Kapanana kadar: **plaket duvara çıkamaz.** |
| Anahtar **`0x02`–`0x04`** | ⚠️ **karara bağlanmadı** | Aynı §8.2.4.2 tavsiyesi kapsamındadır ve aynı saklama sorusuna tabidir (ADR 0017 §6 md. 5). ⚠️ **GERİ KONAN ALINTI (2026-08-20):** bu hücrenin öncülü AN12196 rev. 1.8 §6.16'nın (rev. 2.0: §5.16) *"It is **highly recommended to configure all the Application Keys** during personalization procedure"* cümlesiydi ve bu turda hücre bölünürken **şerhsiz düştü**. Sonucu değiştirmiyor (veri sayfası §8.2.4.2 aynı şeyi *"change all 5 keys"* diye söylüyor) ama bu dosyanın kuralı **tarihsel kayıt silinmez, şerhlenir**. |
| `FileAR.Change` · `FileAR.ReadWrite` | **ikisi de anahtar `0x01`** | 🔴 **KARAR: ADR 0017 §6 md. 13 (2026-08-21, M8-05 FAZ B2b) — bu hücre daha önce *"karara bağlanmadı"* diyordu ve o cümle artık YANLIŞTIR.** Veri sayfası tablo 8: NDEF dosyasında (`02h`) teslimde `Change = 0h`, `Write = ReadWrite = Eh`. Tablo 9: **`Write` ile `ReadWrite` ikisi de** `WriteData`'ya kapı açar → yalnız birini kilitlemek yazmayı kilitlemez; `Change` ise `ChangeFileSettings`'i yönetir, yani teslim değerinde bırakılırsa halka açık anahtar 0 ile ötekiler geri gevşetilebilirdi. **`Fh` (asla) seçilmedi:** Q08 açıkken (host kararlaşmadı) `Fh` her URL düzeltmesini plaket değişimine çevirir ve ADR 0017 §5.3 kurtarmasını dondururdu. **Sıra bozulmuyor:** `Change` teslimde `0h` olduğu için adım 7 hâlâ anahtar-0 oturumunda koşar ve `Change`'i taşıyan komut **o komuttur**. |
| URL parametre adları | **tam olarak** `tag`, `ctr`, `cmac` | ADR 0003 md. 1. Kodda tek yerde sabit: `internal/sun/params.go` (`paramTag`/`paramCtr`/`paramCMAC`). Başka bir ad = ayrıştırma hatası. |
| URL biçimi | `https://<host>/t?tag=<14 hane>&ctr=<6 hane>&cmac=<16 hane>` | ADR 0003 md. 1. |

> ### ✅ BU TABLONUN BAYT KARŞILIĞI ARTIK KODDA — VE NEYİN KANITLI OLDUĞU SAYILI
>
> **2026-08-21 (M8-05 FAZ B2b).** Yukarıdaki satırların hepsi tek bir gövdede
> buluşuyor: `internal/sun/filesettings.go` → `ChangeFileSettingsData`, Tappa
> yapılandırması `TappaNDEFSettings`. Örnek şablon `https://tap.example.com/t`
> için üretilen `CmdData` **18 bayttır**:
>
> ```
> 40 11E1 C1 F1E1 1D0000 300000 3C0000 3C0000
> ^^ FileOption: SDM açık + CommMode plain
>    ^^^^ AccessRights: (ReadWrite 1 <<4)|Change 1 , (Read E <<4)|Write 1
>         ^^ SDMOptions: UID mirror + sayaç mirror + ASCII  (yayımlanmış örnekle AYNI bayt)
>            ^^^^ SDMAccessRights: (RFU F<<4)|CtrRet 1 , (MetaRead E<<4)|FileRead 1
>                 ^^^^^^ UIDOffset 29 · ReadCtrOffset 48 · MACInputOffset 60 · MACOffset 60
> ```
>
> 🔴 **DIŞ ÇAPA VAR AMA BU GÖVDE İÇİN DEĞİL — ayrım kayıt için yazılıyor.** Aynı
> kurucu, AN12196'nın **şifreli-PICC** örneğini (rev. 1.8 §6.9 tablo 19 / rev. 2.0
> §5.9 tablo 18) **birebir** üretiyor: `4000E0C1F121200000430000430000`. Bu,
> **alan sırasını**, iki hak alanının **bayt bileşimini**, `FileOption`/`SDMOptions`
> bitlerini ve 3 baytlık **LSB-first** ofset kodlamasını dışarıdan çiviliyor.
> Tappa'nın plain gövdesi ondan yalnız **iki nibble değeri** ve **hangi ofsetlerin
> var olduğu** ile ayrılıyor — ve o ayrım **yalnız** NT4H2421Gx rev. 3.0 §10.7.1
> tablo 69'dan okunmuş durumda, hiçbir yayımlanmış vektör onu koşturmuyor.
> **Ofsetlerin dosyanın başından (NLEN dahil) sayıldığı** ayrıca ölçüldü: tablo
> 17'nin `WriteData` gövdesinde yer tutucu 32. baytta başlıyor, tablo 18'in
> `PICCDataOffset`'i `200000` = 32.
>
> 🔴 **BAYT SINIRI DA VAR VE İKİ TANE (2026-08-21; ikisi de PDF'ten ölçüldü).**
> Veri sayfası **tablo 81 (s.75)**: `WriteData`'nın veri alanı *"up to **248 byte**
> including secure messaging"* → düz metin tavanı **223 bayt**, ve kod artık
> **birleştirilmiş alanı** ölçüp reddediyor. **§8.2.3.1 (s.10)**: *"The writing
> operations of single frames **up to 128 bytes** with a WriteData or
> ISOUpdateBinary command are also tearing protected."* → 128'in üstündeki bir
> şablon **tearing korumasını kaybeder**, ve yarım yazma tam olarak ADR 0017
> §5.3'ün konusudur. Bu yüzden `BuildTapNDEF` **128 baytın üstünü reddeder**:
> host + path bütçesi **69 bayt**. ⚠️ **Q08 host'u seçerken bu bütçeyi bilin** —
> bugünkü şablon 76 bayt.
>
> 🔴 **128 NEYİ ÖLÇÜYOR — SORULDU VE CEVAPLANDI (kapı kalibrasyonu buna bağlı).**
> **Yazılan baytları**, mühürlenmiş çerçeveyi değil. Kanıt belgenin kendi
> karşıtlığıdır: mühürlenmiş boyutu kastettiğinde **söylüyor** — tablo 81 *"248
> byte **including secure messaging**"* der. §8.2.3.1'de böyle bir ibare **yok** ve
> cümle `WriteData` **ile** `ISOUpdateBinary`'yi birlikte anıyor; `ISOUpdateBinary`
> `CommMode.Plain`'dir (tablo 22), yani onda yazılan bayt = çerçeve baytı. İki
> komut için iki farklı şey ölçen bir sınır, tuhaf okuma olurdu. Kapı bu yüzden
> **dosya uzunluğunda**.
>
> ⚠️ **HÂLÂ SİLİKON HAKEM.** Bu tablo bir çip üzerinde **doğrulanmadı** (ADR 0017
> §6 md. 1). İç tutarlılık çapası var — üretilen URL'yi `internal/sun`'ın kendi
> ayrıştırıcısı ve MAC doğrulayıcısı okuyabiliyor — ama iç tutarlılık **tam olarak**
> M2-08'in dört ay boyunca yeşil kaldığı kanıt sınıfıdır.

> ### 🔴 BOŞ MAC GİRDİSİNİ BİR BUG SANIP UID/ctr EKLEMEYİN
>
> ADR 0003 madde 2'nin uyarısı buraya **taşınıyor**, çünkü hatanın yapılacağı yer
> burasıdır: encode ekranında "MAC input" alanını boş bırakmak yanlış görünür.
> **Boş doğrudur.** Tazelik ve kimlik MAC mesajından değil **session key
> türetiminden** gelir — `SV2` içinde hem UID hem `ctr` vardır. Girdiye veri
> eklerseniz çip yine boş mesajın CMAC'ini yayar, doğrulama **her zaman**
> başarısız olur ve hata *"anahtar yanlış"* gibi görünür — yani günlerce yanlış
> yerde aranır.

> ### ✅ `ctr` BAYT SIRASI — DECODE TARAFI KAPANDI (M2-08), ENCODE TARAFI AÇIK
>
> ADR 0003 madde 1 URL'deki `ctr`'ı **big-endian** olarak sabitler ve sebebini
> yazar: little-endian okumak *sessizce yanlış ama makul* sayılar üretir.
> `internal/sun/params.go` bunu böyle okur.
>
> **SV2'ye giren baytlar ayrı bir eksendir, ve bu eksen artık kapalıdır — donanım
> beklemeden.** AN12196 §4.3 tablo 2 adım 4 (rev. 1.8 s. 10; rev. 2.0'da §3.3
> **tablo 1**, s. 9) `SDMReadCtr`'ı session-key girdisine **LSB-first** koyuyor
> (`010000`) ve **aynı UID** için §4.4.1'in (s. 11) plain SUN URL'i `ctr=000001`
> gösteriyor: tek bir değer, iki yazılış, birbirinin bayt-tersi. `sv2()` bugün URL
> baytlarını **tam bir kez** ters çeviriyor. Kanıt kendi zincirimizden gelmiyor:
> `internal/sun/an12196_kat_test.go` beklenen değerleri belgeden **transkribe
> ediyor** ve kırmızı olduğunda kodu suçluyor, değerleri değil. Bağımsız teyit:
> `icedevml/sdm-backend` plain yolunda sayacı ters çeviriyor, şifreli yolunda
> çevirmiyor. Ayrıntı: **M2-08** — `docs/plan/m2-sun.md` · ADR 0003 ek notu.
>
> ⚠️ **Tek çapa, iki değil.** URL sırasını çivileyen şey **yalnız** Tablo 2 +
> §4.4.1 aynı-UID eşleşmesidir. Belgenin §4.4.4.2.1 tablosu (rev. 1.8 Tablo 5)
> **şifreli-PICCData** örneğidir; UID ve sayacı çözülmüş PICC'ten gelir ve onun
> URL'i `?e=…&c=…` biçiminde, düz `ctr=` **hiç taşımaz**. O tablo zincirin geri
> kalanını (SV2 düzeni, türetme, boş girdi, kısaltma) çiviler — URL sırasını değil.
>
> 🔴 **VE BU KUTUNUN ESKİ HÂLİ KUSURU HAFİFE ALIYORDU — FAZ B'NİN KAPSAMI BU
> YÜZDEN DEĞİŞTİ.** Eski metin vektörlerin hatayı *"göremediğini"* söylüyordu.
> Ölçüldü, gerçek daha ağır: `internal/sun/verify_mac_test.go` içinde adında
> *verbatim* geçen bir SV2 testi vardı, kendi yorumunda *"the load-bearing
> anti-reversal test"* diyordu ve **doğru düzeltmeyi adıyla yasaklıyordu**
> (*"a reversal would make SV2's counter bytes differ from the URL's here"*).
> (Ölü adı buraya **yazmıyoruz**: `cmd/tappa/testnames_test.go` var olmayan test
> adlarına yapılan atıfları sayar ve bu cümle yazılırken **ölçüldü** — adı anmak
> ratchet'i 53'ten 54'e çıkarıp testi kırmızıya çeviriyor. Yerine geçen ad
> `TestSV2_CounterIsReversedIntoLSBFirstOrder`.) Yani
> gerçek çip gelseydi bile doğru yamayı yazan kişi **kırmızı bir test** görecek ve
> büyük ihtimalle yamayı geri alacaktı — donanım tek başına bunu çözmezdi. Ders:
> bir testin **adı** bir iddiadır ve yanlış iddia, eksik testten pahalıdır.
>
> **Geriye kalan ENCODE tarafıdır.** AN12196 bizim **decode** tarafımızı çipin
> tarifine bağlar; encode aracımızın sayacı URL'ye **MSB-first** mirror'ladığını
> kanıtlayamaz. Bu yüzden aşağıdaki FAZ B listesinin 1. maddesi **silinmedi,
> daraltıldı**.

### Anahtar teslimi ve döndürme

**Teslim.** Çipler tedarikçiden **fabrika varsayılanı** anahtarlarla gelir
(veri sayfası §8.2.4.2: *"The transport value of these 5 keys is 16 bytes of
00h"*). Encode sırasında `K_SDMFileRead` (**anahtar `0x01`**) bizim ürettiğimiz
rastgele anahtarla değiştirilir (ADR 0003 md. 5), ve **uygulama anahtarı `0x00`
da kişiselleştirilir — en son adım olarak** (ADR 0017 §5.0 karar 2; sevkiyatı bir
şema kararına bağlı, aynı ADR §6 md. 5). Anahtar `0x02`–`0x04` **karara
bağlanmadı**. Ayrıntı: yukarıdaki tablonun ilgili satırları.
Varsayılan anahtarla hiçbir plaket üretime çıkmaz — bu, Q10 kararının (kendimiz encode
ederiz) **operasyonel karşılığıdır**.

**Döndürme — ve burada iki AYRI şey var, karıştırmayın.** Bu ayrım bu dosyanın
"KEK döndürme" bölümündeki *"Ne döndürülüyor — ve ne döndürülMÜyor"* tablosuyla
**birebir aynı** olmak zorundadır:

| Sızan ne? | Yol | Nerede yazılı |
|---|---|---|
| **KEK** (`TAPPA_TAG_KEK`) | Park yeniden sarmalanır; **duvara kimse gitmez**. `cmd/rotatekek` + üç adımlı prosedür. | Bu dosya → **"KEK döndürme"** |
| **Plaketin kendi AES anahtarı** | **`retire + replace`** — ilgili plaket `status='retired'` olur, **yeni UID + yeni rastgele anahtar** taşıyan yeni bir plaketle **fiziksel olarak** değiştirilir. | ADR 0003 **md. 5** |

🔴 **Yerinde `ChangeKey` ile plaket anahtarı döndürmek MVP DIŞIDIR.** ADR 0003
madde 5 bunu *"teknik olarak mümkün ama MVP kapsamı dışı"* diye adlandırıyor ve
normatif yolu **`retire + replace`** olarak veriyor. Encode aracı (yazıldığında)
`ChangeKey`'i **yalnız boş çipi kişiselleştirmek için** kullanacaktır (fabrika
varsayılanından bizim anahtarımıza); bu, **sahadaki bir plaketi yeniden
anahtarlamak** ile aynı şey değildir. Aynı komut, iki farklı operasyon — ve
ikincisi yazılı değil.

Toplu yeniden-anahtarlama **yoktur ve olamaz**: park geneli bir master anahtar
yok (ADR 0003 md. 3), bu yüzden şüphe daima **tekil** ele alınır. Patlama yarıçapı
tek plakettir; bedeli ise merdivendir.

### Anahtar hijyeni — iddia değil, mekanizma

*"Anahtarlar repoya, sohbete, e-postaya yazılmadı"* bir söz değil, **kontrol
edilebilir bir zincir** olmalı. Bugün yürürlükte olan mekanizmalar:

1. **Düz anahtar hiç kalıcılaşmaz.** `internal/sun`'da `Wrap(kek, uid, key)`
   AES-256-GCM zarfını üretir (AAD = **ham 7 baytlık UID**, yani sarmalı anahtar
   kendi satırına **bağlıdır**, başka satıra taşınamaz) ve `Zero(key)` düz
   kopyayı siler. Encode aracı bu ikisini **aynı süreçte** çağırmak
   **zorundadır** (araç yazıldığında; bugün yok — bölüm başındaki uyarı).
   ⚠️ **DÜZELTME (2026-08-20, ADR 0017 §3).** Burada *"aynı süreçte, **art
   arda**"* yazıyordu ve **art arda olan yarısı yanlıştı**: `Wrap` düz anahtarı
   kalıcılaştırır ama **tüketmez** — `ChangeKey` gövdesini kurabilmek için düz
   anahtar **altı APDU turu daha** bellekte kalmak zorunda (ADR 0017 §5.1: `Wrap`
   adım 3, `ChangeKey` adım 6/8, `Zero` adım 9). Ayakta kalan şart **aynı
   süreçtir**; düşen şart **bitişikliktir**. Bunun bedeli, `Zero`'nun **her**
   çıkış yolunda (başarı · hata · zaman aşımı · iptal · kapanış) çağrılmasının
   ayrıca garanti edilmesidir — ADR 0017 §6 md. 7.
   ✅ **VE O BEDEL ÖDENDİ (2026-08-21, FAZ B2c-1).** ADR 0017 §6 md. 7 **kapandı**
   ve yerini alan şey nesir değil **mekanizma**: `internal/encode`'da TTL **90 sn**
   (tabanı adım tablosundan türetilmiş, tavanı iki katı) · `defer` **panik
   yolunda** · süpürücü **iki yollu** (tembel + goroutine) · **tek çıkış**
   (`retireLocked`, 11 çağrı yeri `go/ast` envanteriyle çivili) · ve silme
   garantisinin ifadesi **yerden bağımsız**, **sayılmış açığıyla** birlikte
   (`Session`'a yeni alan **mekanik**; başka her dinlenme yeri — özellikle **map
   anahtarı**, ki bir Go dizesi **hiç sıfırlanamaz** — **yakalanmıyor**).
   ⚠️ **Niteleyici, ve bu maddenin kendi parantezini bozmaz:** mekanizma sevk
   edildi ama **hiçbir yerden import edilmiyor** (ölçüldü: paket dışı import **0**),
   yani *"araç yazıldığında; bugün yok"* **akışın bütünü için hâlâ doğrudur**.
   Kapanan şey **yükümlülüğün yazılmamış olması**dır.
2. **Veritabanı yalnız sarmalıyı görür.** `tags.aes_key_ref` = 44 bayt
   (`nonce(12) ‖ ciphertext(16) ‖ gcm_tag(16)`), ADR 0003 md. 4.
3. **Uygulama rolü onu DEĞİŞTİREMEZ.** Migration 00013 `tappa_app`'e `tags`
   üzerinde **sütun listeli** UPDATE veriyor (`location_id`, `last_ctr`,
   `status`, `retired_at`, `replaced_by`) — `aes_key_ref` **listede yok**.
   ⚠️ **DÜZELTME (2026-08-20, ADR 0017 §3.1).** Bu madde *"yazamaz"* diyordu ve
   *"Satırı `tappa_owner` yükler"* diye bitiyordu; ölçüldü, bu **UPDATE için
   doğru, INSERT için değil**: 00004'ün `GRANT SELECT, INSERT, UPDATE ON tags TO
   tappa_app` satırındaki **tablo geneli INSERT** hiç geri alınmadı (00013 bunu
   bilerek yaptı ve *"the day a loader writes plaques through the application
   role, this line is what has to be revisited"* diye yazdı). Bugüne kadar
   *"owner yükler"*in doğru **görünmesinin** sebebi tek yükleyicinin
   (`scripts/seed.sh` + `seedkeys`) owner olarak koşmasıydı — yapısal bir engel
   değil, bir alışkanlık. Encode akışı bir HTTP uç noktasıdır ve `internal/db`
   havuzu **üretimde** owner bir DSN'i **reddettiği** için (`roleRefusal(facts,
   cfg.IsProd())` — geliştirmede **uyarır, reddetmez**) satırı **`tappa_app`
   yazacaktır**.
   Ayakta kalan garanti: anahtar referansı **yaz-bir-kez**tir (INSERT edilebilir,
   asla UPDATE edilemez) ve `tags` politikası `WITH CHECK` ile satırı çağıranın
   tenant'ına bağlar. Ayrıntı ve gerekçe:
   [ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §3.1.
4. **Panel plaket yaratamaz.** `db/queries/tags.sql` **hiç INSERT taşımıyor** ve
   bu bir eksiklik değil karardır (kullanıcı kararı 2026-08-08: *"Tappa encodes
   the plaque and loads the row; the panel only binds it"*). Plaket doğuran tek
   yol, bu runbook'un tarif ettiği encode akışıdır — **ve o akışı koşan araç henüz
   yazılmadı**, yani bugün üretimde encode edilmiş plaket **yoktur**.
5. **Mekanik tarama.** `scripts/redline-check.sh` R7 iki şeyi arıyor: repoda
   gömülü anahtar dosyası (`*.pem`, `*.key`, `*.aes`, `secrets/*`) ve sır taşıyan
   log çağrısı. ⚠️ Geçmesi ihlal olmadığını **kanıtlamaz** (CLAUDE.md §4).
6. **Emsal: KALICILAŞAN yalnız SARMALI blob'dur.** `test/fixtures/seedkeys`
   KEK'i ortamdan okur, asla basmaz; stdout'a yalnız sarmalı değeri taşıyan SQL
   yazar. Encode aracının **veritabanına yazdığı** şey aynı şekli almalıdır.
   🔴 **DÜZELTME (2026-08-20, ADR 0017 §2.2/§3).** Bu madde *"yalnız sarmalı blob
   **çıktıya** çıkar"* diyordu ve encode aracına *"çıktısı aynı şekli almalıdır"*
   diye emrediyordu; **encode aracı bunu SAĞLAYAMAZ ve sağlaması da beklenmemeli**.
   `seedkeys` bir **filtredir** — tek çıktısı SQL'dir. Encode aracı bir
   **rölenin sunucu ucudur**: telefona **APDU baytları** da gönderir, ve
   `ChangeKey`'in gövdesi bunların arasındadır. Boş bir çipte o gövdeyi koruyan
   oturum anahtarları **halka açık fabrika anahtarından** türediği için döküm
   anahtara **eşdeğerdir** (ADR 0017 §2.2 — kabul edilmiş risk, ADR 0005 risk 7).
   Ayakta kalan emsal: **kalıcılaşan** hiçbir şey düz değildir.
   🔴 **VE BU MADDENİN DÜŞÜRDÜĞÜ MEKANİK SINIR HÂLÂ YERİNE KONMADI — SAYILIYOR
   (2026-08-21, FAZ B2c-1).** ADR 0017 §6 md. 14 bunu *"sayılmış kayıp"* olarak
   kaydetmişti ve *"turun 2'si mekanik bir şeyle değiştirmeli"* diyordu; **turun 2
   bitti ve değiştirmedi**. Ölçüldü: süreçten **çıkanı** bağlayan **genel** bir kapı
   yok. Bugün var olan üçü **adlandırılmış kanalları** bağlıyor, iddiayı değil —
   `internal/encode` **hiç logger içermiyor** (kaynak okuyan test) · **hata
   mesajları** anahtar baytı taşımıyor (test) · `redline-check.sh` **R7** gömülü
   anahtar dosyası ve sır taşıyan log çağrısı arıyor. Paket bugün **hiçbir şey
   yazmıyor** (dosya/stdout yazıcısı **sıfır isabet**), ama bu **bugünkü kodun
   özelliğidir, bir kapı değil**. Devir listesine adıyla girdi.

**Operatör kuralları (mekanizmanın kapatamadığı yarı).**
- Düz anahtar **hiçbir zaman** ekrana basılmaz, panoya kopyalanmaz, sohbete/
  e-postaya yapıştırılmaz, ticket'a eklenmez.
- Encode aracı çalışırken çıktısı bir dosyaya **yönlendirilmez**; SQL doğrudan
  yükleyiciye gider ya da 0600 izinli geçici bir dosyaya yazılır ve iş biter
  bitmez silinir.
- Doğrulama **değer göstermeden** yapılır: uzunluk (44 bayt), satır sayısı,
  eşitlik boolean'ı — hex dökümü değil.

### Plaket baskısı

**Tasarımın tek yetkili kaynağı skill `tappa-brand` → "Plaket baskısı — NFC + QR
(fiziksel yüzey)".** Palet, tipografi, yerleşim oranları, QR kenar uzunluğu,
sessiz alan ve metinler orada; burada **tekrarlanmaz** (iki yerde duran bir ölçü
er geç iki farklı ölçü olur). ⚠️ O bölüm kendi başlığında *"bu bir TASARIM
ÖNERİSİDİR, ölçüm değil"* diyor — milimetreler **baskı provasında** doğrulanacak.

⚠️ **Kâğıt boyu O SKILL'DE YAZMIYOR — atfı doğru yere ver.** Ölçüldü:
`.claude/skills/tappa-brand/SKILL.md` içinde `A5`, `148` ve `210` **hiç geçmiyor**
(0 isabet). Skill'in gerçekten söyledikleri şunlar: plaketin **dikey ~%60/%40**
bölünmesi, **QR kenarı ≥ 30 mm** (20–40 cm tarama mesafesinin 1/10'u), **4 modül
sessiz alan**, `ink` on `paper` kontrast ve metinler. **A5 kararı skill'den değil
üründen gelir**: [handoff.md](../docs/handoff.md) → *"markalı, **A5 boyutlu**,
kameranın gördüğü bir noktaya monte edilen pasif bir plaket"*; M8-05 kartının 6.
kriteri onu tekrarlar. `148 × 210 mm` ise A5'in ISO 216 tanımıdır, bu repoda bir
kaynağı yoktur ve **prova ile doğrulanacaktır**.

Kartın istediği üç şey ve bugünkü durumları:

| Kriter | Durum |
|---|---|
| **A5** (ISO 216: 148 × 210 mm) — kaynak **handoff.md**, skill değil | Skill'in dikey **%60/%40** bölünmesiyle uyumlu; kâğıt boyunun kendisi **prova ile doğrulanacak**. |
| **NFC + QR birlikte** | Zorunlu, ve QR *"yedek"* değildir — iPhone X ve öncesi arka planda NFC okuyamaz, o çalışan için QR **her günkü** yoldur. |
| **Kamera görüş alanına montaj** | QR **kamerayla** okunur: plaket, telefonun kamerasının **engelsiz** göreceği yükseklik ve açıda monte edilir; NFC anteni QR'ı kapatmayacak şekilde ayrı bölgede durur. |

> #### 🔴 AYNI PLAKETTE QR OLMASININ §5'TE BİR BEDELİ VAR — VE BASKI BUNU BİLMELİ
>
> QR **statiktir**: `ctr` ve `cmac` taşımaz, çip onu her okumada yeniden yazmaz.
> Yani QR'da **proof of moment yoktur**. `internal/sun.Parse` bu ikisinin
> **ikisinin birden** yokluğunu `Channel=qr` olarak okur ve karar baseline
> politikası **`base:qr-requires-ip`** ile ilerler: QR tap'inde **IP zorunludur**,
> GPS tek başına yetmez → aksi hâlde `flag`.
>
> **Sebebi doğrudan baskıyla ilgilidir:** duvardaki QR **fotoğraflanabilir** ve o
> fotoğraf **süresiz geçerli** bir bağlantıdır. NFC'de bir sonraki tap yeni bir
> fiziksel dokunuş ister; QR'da istemez. Baskı bunu ortadan kaldıramaz — bu
> yüzden mekânın **statik IP'si** QR yolunun tek gerçek çıpasıdır ve M8-07'nin
> "statik IP'ler müşteriden teyitli" kriteri QR basılan her lokasyon için
> **kritiktir**.
>
> **İzleme parametresi eklenmez.** Baskı sağlayıcısı `?src=qr` benzeri bir işaret
> önerirse cevap **hayır**: kanal, `ctr`/`cmac`'in **yokluğundan sunucuda**
> türetiliyor; URL'e istemcinin taşıdığı bir kanal işareti eklemek o türetimi
> ikinci bir kaynakla yarıştırırdı.

> #### 🔴 Q08 KAPANDI — AMA ENCODE EDİLEN HOST HÂLÂ GERİ ALINAMAZ
>
> SUN URL'si **domaini taşır** ve domain artık **alındı: `taptime.mt`**
> (Cloudflare zone aktif, 2026-09-02 — `open-questions.md` Q08 kapandı). Encode
> edilecek host **`taptime.mt`**'dir.
>
> Çipe yazılan NDEF, o çipin anahtarı olmadan **değiştirilemez**; anahtarı olsa
> bile her plaket için **tek tek, fiziksel olarak** yeniden yazmak demektir. Yani
> **yanlış host'la encode edilmiş bir plaket = sahada plaket değişimi.** ADR 0003
> girişinde bu maliyeti *"yanlış modda veya yanlış anahtar stratejisiyle"* encode
> için adlandırıyor; **host üçüncü hâlidir** ve ADR'de yazmıyor — burada yazıyor.
>
> **Kural: üretim plaketi yalnız `taptime.mt` host'uyla encode edilir** — Q08
> kapandığı için önü açık. Encode edilen plaket duvara bağlanana kadar
> `status='unassigned'` kalır.

### Encode edilen satırın durumu

Encode biten bir plaketin satırı **`status='unassigned'`** ile yüklenir,
`location_id` **NULL**. Bu, migration 00013'ün envanter modelidir: Tappa plaketi
**encode eder ve yükler**, müdür panelden **duvara bağlar**. `active` yazmak,
kutuda duran bir plaketi hizmette göstermek olur — ve `internal/sun`'ın akışı
`active` olmayan her plaketi §5 satır 1'den reddeder, yani yanlış durum sessiz
kalmaz ama **yanlış yönde** de yanılmaz.

### 🔴 YANLIŞ YÜKLENMİŞ BİR SATIR NASIL TEMİZLENİR — TEK YOL ELLE MÜDAHALEDİR

> **Eklendi 2026-08-24 (M8-05 FAZ B2c-2b).** Encode akışı artık bir HTTP uç
> noktasıdır ve `tags`'a **üretimde** satır yazar. ADR 0017 §6 md. 12 üç azaltma
> adlandırıyor — uç noktada yetkilendirme, oran sınırı, **ve bu bölüm**: bugün tek
> temizlik yolunun elle müdahale olduğunun operatörün bulacağı yerde **yazılı**
> olması. İlk ikisi sevk edildi; bu, üçüncüsüdür.

**Ne oluyor.** `tags.uid` **global PRIMARY KEY**'dir, tenant kapsamlı değildir — ve
bu **doğrudur**: bir tap geldiğinde elde yalnız URL'deki uid vardır, tenant o
aramanın **sonucudur** (00004'ün kendi yorumu, ADR 0002 md. 7). Yan etkisi şudur:
bir uid'i **hangi tenant yazarsa yazsın**, o uid **park geneli için tükenir**.

**Üç sonucu, üçü de ölçüldü** (`tappa_app`, `BEGIN … ROLLBACK`):

| Deneme | Sonuç |
|---|---|
| B tenant'ı, A'nın var olan uid'siyle `INSERT` | `23505 duplicate key` — oysa **aynı bağlantıda** `SELECT` o uid için **0 satır** döner (RLS satırı gizler, PK varlığını ele verir) |
| B taze bir uid'i işgal eder, A onu ister | A **göremez** (0 satır), **yazamaz** (23505), **silemez** (`permission denied`) |
| Satır düzeltilebilir mi | **Hayır.** `aes_key_ref` **yaz-bir-kez**: 00013 `tappa_app`'ten `UPDATE`'i geri aldı ve sütun listesine koymadı |

**Yani düzeltme uygulamadan yapılamaz.** Panelde "plaketi sil" diye bir düğme
**yoktur ve olmayacaktır** (§4.6 ailesi: kayıt silinmez); `tappa_app`'in `tags`
üzerinde `DELETE` yetkisi **yoktur** (00004).

**Elle müdahale — `tappa_owner` ile, ve önce DUR:**

1. **Silmeden önce sor: gerçekten yanlış mı?** Yarıda kalmış bir tur satırı
   `status='unassigned'`, `location_id IS NULL` bırakır — bu **hizmette değildir**,
   `internal/sun` akışı onu §5 satır 1'den zaten reddeder, ve **kurtarılabilir**:
   sarmalı anahtar elimizdedir, aynı anahtar çipe yeniden sürülebilir. Ölü bir
   envanter satırı **zararsızdır**; silmek değildir.
2. **Silinmesi gereken tek şekil:** yanlış tenant'a düşmüş ya da hiç var olmayan bir
   çipin uid'ini işgal eden satır. İkisi de bir **insan kararıdır**.
3. **Nasıl:** `DATABASE_MIGRATE_URL` (yani `tappa_owner`) ile, tek bir
   `BEGIN … DELETE … <önce SELECT ile doğrula> … COMMIT` içinde, ve **uid birebir
   yazılarak**. `tappa_app`'in DSN'i bu işi yapamaz — bu bilerek böyledir.
4. **`audit_log` satırları KALIR.** `plaque.loaded` (ve varsa `plaque.encoded` ya da
   **`plaque.unmarked`** — sonuncusu *"çip kişiselleştirildi ama satır işaretlenemedi,
   TEKRAR ENCODE ETME"* demektir ve bu ayrımı yalnız o satır taşır.
   🔴 **VE ARTIK NADİRDİR — GÖRDÜYSEN VERİTABANI ERİŞİLEBİLİRDİ** (bu satır yazılabildi
   demek, havuz bağlantı verdi demek; kutunun altındaki özellikten **türetilir**.
   ⚠️ Bir önceki yazımı *"GERÇEKTEN ERİŞİLEMEZDİ"* diyordu ve **yedi satır aşağısıyla
   çelişiyordu**). Telefonun
   son R-APDU'yu gönderip bağlantıyı kapatması **bu satırı ÜRETMEZ**: o durum artık
   isteğin context'inden kopuk bir yazma ile **onarılıyor** (`encoded_at` damgalanır,
   tur normal biter). Bu satır yalnız **gerçek** bir arızada yazılır — ve hangi
   arızalarda yazılabildiği aşağıdaki özellikle belirlenir, bir liste ile değil.
   🔴 **VE BU SATIRIN YOKLUĞU HİÇBİR ŞEY KANITLAMAZ — BUNU BİR LİSTE OLARAK DEĞİL,
   BİR ÖZELLİK OLARAK OKU:**
   > **Telafi kaydı, hakkında rapor verdiği yolun ÜSTÜNDEN geçer** — aynı havuz, aynı
   > veritabanı, aynı süreç. Bu yüzden yalnızca **o yolu sağlam bırakan** bir arızayı
   > kaydedebilir. Yolun **kendisindeki** her arıza, telafiyi de beraberinde götürür.
   🔴 **BUNDAN TÜRET, YENİDEN SAYMA.** Paylaşılan şey **havuz · veritabanı · süreç**;
   **son tarih paylaşılmaz** (`WithoutCancel` ebeveynin deadline'ını düşürür, telafi
   **taze bütçeyle** başlar). Dolayısıyla:
   **(1) satır VARSA** → veritabanı erişilebilirdi; taşıdığı arıza kısıt reddi, mantık
   hatası, ya da **yalnız işaretlemenin kendi bütçesinin dolması** olabilir.
   **(2) satır YOKSA** → hiçbir şey kanıtlanmaz; havuz tükenmesi, ağ kopması, ölü
   veritabanı ve öldürülen süreç telafiyi de düşürür.
   ⚠️ **VE HATA METNİ İKİ SINIFI AYIRT ETMEZ — koşula bak, mesaja değil.** Aynı
   `db: begin tx: context deadline exceeded` iki zıt sonuç veriyor (ölçüldü):
   `sağlam havuz + işaretlemenin bütçesi dolmuş` → **`plaque.unmarked`=1 (KAYDEDİLDİ)** ·
   `pool_max_conns=1, havuz doygun` → **satır YOK**, `encoded_at` **NULL**, plaket
   **dokunulmamış görünüyor**. Ayıran şey **havuzun bağlantı verip veremediği**.
   ⚠️ **O YÜZDEN ŞÜPHEDE SATIRA DEĞİL, ŞU İKİLİYE BAK** — `tags` içinde `aes_key_ref`
   **dolu** ama `encoded_at` **NULL** olan satırlar: bu ikili, tamamlanmamış ya da
   **kaydedilememiş** bir turun izidir ve telafi kaydından **bağımsızdır**. Bunu
   sistematik yapan idempotent bir uzlaştırma geçişi bugün **yok** — devir listesi
   **md. 27**)
   append-only bir tablodadır ve **`tappa_owner` bile silemez** (00005'in trigger'ı).
   Yani bir plaket satırı silinse bile *"kim, ne zaman, hangi tenant için yükledi"*
   izi durur — `detail.claimed_by` yükleyen yöneticinin id'sini taşır.
5. **Managed Postgres'te bu prosedürün yürütülebilir yolu var mı diye ayrıca bak** —
   aynı sınır KEK döndürme bölümünde de sayılı.

⚠️ **Bu bir mekanizma değil, bir prosedürdür — ve öyle yazılmıştır.** Uç noktadaki
yetkilendirme (`AdminAuth.ProtectWriting` + oturumdan gelen tenant) ve oran sınırı
(`adminEncodeLimit`) işgali **bir kimliğe bağlar ve sayısını sınırlar**; hiçbiri onu
**imkânsız** kılmaz. ADR 0017 §6 md. 12 kapatılmadı, **sayıldı**.

### FAZ B'ye devredilenler — yükümlülük listesi

Donanım (bir NTAG 424 DNA yazıcı/okuyucu) geldiğinde yapılacaklar. Bu liste
**M8-05'in kapanma koşuludur**; FAZ A tek başına kartı kapatmaz.

1. **`ctr` bayt sırasının ENCODE yarısı gerçek çiple doğrulanır.** ⚠️ **Bu madde
   M2-08 ile DARALDI, kalkmadı.** Kapanan yarı: **decode** — `sv2()` URL baytlarını
   tam bir kez ters çeviriyor ve bu artık dış kaynaklı known-answer vektörleriyle
   çivili (`internal/sun/an12196_kat_test.go`, AN12196'dan transkribe). Bu yüzden
   madde **artık diğerlerinden önce koşulmak zorunda değil**. Kalan yarı: encode
   aracının sayacı URL'ye gerçekten **MSB-first** yazdığını **silikonla** görmek —
   ADR 0003 madde 1 bunu karar olarak sabitliyor, vektörler ise **varsayıyor**.
   Gerçek çipten alınan **tek bir** tap URL'si yeter ve sinyal gürültüsüzdür:
   sayaçların 255/256'sı palindrom değil, yani encode tarafı ters yazıyorsa
   doğrulama **her tap'te** başarısız olur.
2. **Araç yolu seçilir ve kurulur** (tam Go yazıcı vs üçüncü parti yazıcı +
   yükleyici). Taşıma katmanı yeni bir bağımlılık gerektiriyorsa **CLAUDE.md §1
   gereği onay alınır**; FAZ A hiçbir bağımlılık eklemedi.
   ✅ **SEÇİM → ADR 0017 ile kapandı (2026-08-20): B yolu (kendi Android
   uygulamamız, APDU rölesi); onay Android dil zinciri içindir, yeni bir Go
   bağımlılığı için değil.** ⚠️ **KURULUM AÇIK** — tek satır kod yazılmadı.
3. **En az bir fiziksel etiketle uçtan uca doğrulama** (kart kriteri 3): encode →
   duvara benzet → gerçek telefonla tap → kayıt panelde görülür.
4. **Gerçek çiple replay denemesi** (M8-04 kartı bunu istiyor): aynı tap URL'si
   ikinci kez gönderilir → **reject** beklenir; `tags.last_ctr` tek adım ilerlemiş
   olmalıdır.
5. **Q11 — iPhone Safari çerez ömrü** (`open-questions.md`: *"gerçek cihaz turu
   M8-05'te"*). Ölçülecek olan: **sunucunun yazdığı httpOnly** çerez ITP altında
   1 yıl yaşıyor mu (7 günlük kırpma **JS ile yazılan** çerezlere ait — doğrulanması
   gereken tam olarak bu ayrım). *"Telefon seni tanır"* vaadi buna dayanıyor.
6. **Fabrika varsayılanlarının gerçekten değiştiği doğrulanır — VE BU MADDE
   ANAHTAR NUMARASINA GÖRE İKİYE AYRILIR (düzeltme 2026-08-20, ADR 0017).**
   Eski hâli *"varsayılan anahtarla kimlik doğrulama **başarısız** olmalı"*
   diyordu ve ADR 0017 §5.3 tarafından **yanlışlanıyordu**; aynı deneyin iki zıt
   beklentisi iki dosyada normatif duramaz.
   - **6a — anahtar `0x01` (`K_SDMFileRead`):** encode sonrası **fabrika
     varsayılanıyla** `AuthenticateEV2First(0x01)` **BAŞARISIZ**, satırdaki
     anahtarla **BAŞARILI** olmalı.
   - **6b — anahtar `0x00` (AppMasterKey):** beklenti **§6 md. 5'in şema
     kararına bağlıdır**. Karar kapanana kadar `AuthenticateEV2First(0x00,
     fabrika)` **BAŞARILI olacaktır** ve bu bir arıza değil, **bilinen ve
     aşağıda 9. maddede sayılan bir maruziyettir**. Karar kapandıktan sonra
     beklenti tersine döner.
7. **Yazma izninin gerçekten kilitli olduğu doğrulanır — İKİ DENEY, BİRİ DEĞİL
   (düzeltme 2026-08-20, ADR 0017).** Eski hâli yalnız birincisini ölçüyordu.
   - **7a — kimlik doğrulamasız** bir NDEF yazma denemesi **reddedilmeli**
     (`FileAR.Write` ve `FileAR.ReadWrite` anahtara bağlandıktan sonra).
   - **7b — kimlik doğrulanmış:** halka açık **fabrika anahtarı 0** ile
     `AuthenticateEV2First` yapıp `WriteData` denemesi. Bugün **başarılı
     olacaktır** — ADR 0017 §5.0'ın oltalama vektörü tam olarak budur. 9. madde
     kapanınca bu deney **reddedilmeli**.
8. **Baskı provası** — A5 ölçüleri, QR kenar uzunluğu ve sessiz alan **fiziksel
   provada** doğrulanır; doğrulanınca skill `tappa-brand`'in ilgili bloğu
   *"ölçüldü"* olarak güncellenir.
9. 🔴 **ANAHTAR 0 MARUZİYETİ KAPATILIR — ve bu madde 2026-08-20'de eklendi çünkü
   listede onu yakalayacak tek satır yoktu.** ADR 0017 §5.0 anahtar 0'ın
   kişiselleştirilmesini **karara bağladı** ama sevk edilmesini bir **şema
   kararına** bağladı (§6 md. 5: ikinci anahtarı nerede saklayacağız).
   Kapanana kadar encode edilen her plakette uygulama anahtarı 0 **fabrika
   varsayılanındadır**, yani **halka açıktır**.

   **Somut sonucu:** plakete fiziksel erişimi olan biri master olarak kimlik
   doğrulayıp NDEF URL'sinin host'unu değiştirebilir → çalışan plakete dokunur,
   telefonu **saldırganın sitesini** açar. Veri sayfası bunu kendi ayak notunda
   söylüyor (tablo 8, s. 12): *"Write and ReadWrite access rights for the NDEF
   File (File No. 02h) **should be changed after personalization** in order to
   prevent unauthorized changes in the NDEF File."* ⚠️ **Ve bu teorik değil:**
   bu dosyanın *"Dört yol"* ölçümü **NFC.cool Tools**'un telefonda `ChangeKey`
   **ve** `ChangeFileSettings`'in ikisini de yaptığını kaydetti — gereken şey
   özel donanım değil, bir mağaza uygulaması.

   > 🔴 **GÜVENLİK ÇİZGİSİ — İKİ AYRI KAPI, KARIŞTIRMAYIN.**
   >
   > **Encode aracının inşası ve testi bu maddeyi BEKLEMEZ.** Geliştirme ve
   > deneme plaketleri anahtar 0 fabrika varsayılanındayken encode edilebilir;
   > tezgâhta, kutuda, laboratuvarda sorun yoktur.
   >
   > **Ama bir plaket, anahtar 0 fabrika varsayılanındayken DUVARA ÇIKAMAZ.**
   > Bu bir **pilot bloklayıcısıdır** (M8-06), encode bloklayıcısı değil — ve
   > Q08'in *"Q08 kapanmadan üretim plaketi encode edilmez"* kuralıyla aynı
   > sınıfa aittir: ikisi de üretim plaketini durdurur, geliştirmeyi durdurmaz.

   **Üç saklama şıkkı — SAYILDI, SEÇİLMEDİ** (turun 2'si şemayı önünde görerek
   seçsin). Her birinin ADR 0017 §5.3'ün teşhis sondalarına etkisi yazılı:

   | Şık | Bedeli | §5.3 sondalarına etkisi |
   |---|---|---|
   | **1. Yeni sütun + migration** | CLAUDE.md §6 beşlisi (tenant sütunu, RLS, FORCE, politika, indeks) + GRANT; `tags`'a ikinci bir sarmalı anahtar alanı | ✅ **Sondalar korunur ve GÜÇLENİR** — sonda 3, *"bizim anahtar 0'ımızla dene"* hâline gelir ve adım 8'in koşup koşmadığını kesin ayırır |
   | **2. Aynı plaket sırrından KDF ile türetme** (`key0 = KDF(plaketAnahtarı, …)`) | 44 bayt **sabit kalır**, şema borcu yok, anahtar 0 istendiğinde geri getirilebilir. 🔴 **Bedeli ADR 0017 §5.0 karar 1'in 2. gerekçesiyle DOĞRUDAN çarpışır:** iki farklı yetki (SDM okuma · kart master) **tek sırra kenetlenir**, yani sızan bir plaket anahtarı yalnız SUN üretmez, o çipin NDEF'ini ve tüm anahtarlarını da açar. ✅ **ADR 0003 md. 3 BOZULMAZ** — türetilen sır **plaket-başına** kalır, park geneli bir master doğmaz, patlama yarıçapı **tek plaket** | ✅ Sondalar korunur (anahtar 0 hesaplanabilir) |
   | **3. Yaz-ve-saklama** (anahtar 0'a rastgele yaz, değeri **at**) | Sıfır şema borcu. ADR 0003 md. 5'in normatif yolu (`retire + replace`) ile **tutarlı**: o çipe bir daha kimlik doğrulanamaz, tek çare değişimdir | 🔴 **SONDA 2 VE 3 ÇALIŞMAZ HÂLE GELİR.** Anahtar 0 bilinmediği için hiçbir kimlik doğrulama kurulamaz → **yarım-yazma teşhisi tamamen kaybolur**, yarıda kalan her çip doğrudan çöp olur. Bu, ADR 0017 §5'in tüm kurtarma yolunu iptal eder |

---

## KEK döndürme — `TAPPA_TAG_KEK` sızdıysa

> **Bu bölüm bir prosedürdür, bir açıklama değil.** Panikteyken okunacak diye
> yazıldı: önce ne döndürdüğü, sonra ön koşullar, sonra sırayla komutlar, sonra
> doğrulama, sonra **geri alma**, sonra **ne zaman durulacağı**.

### Ne döndürülüyor — ve ne döndürülMÜyor

| | |
|---|---|
| **Döner** | **KEK.** Her plaketin AES anahtarı **yeni KEK altında yeniden sarmalanır**. `tags.aes_key_ref` değişir. |
| **Dönmez** | **Plaketin kendi AES anahtarı.** O çipe yazılıdır (**ADR 0003 md. 5 — "Anahtar döndürme"**; md. 3 anahtarın nasıl ÜRETİLDİĞİNİ anlatır). Değiştirmek **her duvardaki her plaketi fiziksel olarak yeniden encode etmek** demektir; ⚠️ ve md. 5 şüpheli bir plaket anahtarı için normatif yolu **`retire + replace`** olarak adlandırıp yerinde **`ChangeKey`'i MVP DIŞI** bırakıyor — yani bu satırın tarif ettiği şey ADR'nin **bilerek ertelediği** mekanizmadır. Bu prosedürün kapsamı DIŞINDA, bir saha operasyonudur. Anahtarın nasıl ÜRETİLİP çipe yazıldığı ve `retire + replace`'in operasyonel karşılığı: bu dosya → **"Plaket encode — boş çipten duvara"**. |
| **Değişmeyen** | **Duvardaki çip.** Kimse bir mekâna gitmez. |

Yani: **sızan bir KEK bir terminalden kurtarılır; sızan bir PLAKET ANAHTARI merdivenle
kurtarılır.** İkisini karıştırma — sızıntının hangisi olduğunu önce belirle.

### Bu iş neden tek adım değil — karışık park

Parkın hangi satırının hangi KEK'le sarıldığını **şemada hiçbir şey yazmıyor**
(`tags`'te KEK sürüm sütunu **yok**, bilinçli: eklenseydi de her iki anahtarın süreçte
**mevcut** olması yine gerekirdi, yani bir migration alır bir güvenlik vermezdi).
Dolayısıyla yeniden sarmalama **anlık değildir** ve sürerken park **karışıktır**:
bir kısmı yeni KEK altında, bir kısmı eski.

🔴 **Tek KEK tutan bir süreç, karışık parkın öteki yarısındaki her tap'e 500 döner.**
Bu §4.6'nın (*"kayıt asla kaybolmaz"*) ürünün ALTINDA çökmesidir: kayıt `flag`'lenmez,
**hiç alınmaz**. Bu yüzden sıra şudur ve **atlanamaz**:

```
1. İKİ KEK'İ DE YÜKLE      (TAPPA_TAG_KEK_PREVIOUS = eski, TAPPA_TAG_KEK = yeni) → rollout
2. PARKI YENİDEN SARMALA   (cmd/rotatekek)
3. ESKİ KEK'İ DÜŞÜR        (TAPPA_TAG_KEK_PREVIOUS'u kaldır) → rollout
```

**Adım 3 rotasyonun kendisidir.** 1 ve 2 bittiğinde sızmış KEK **hâlâ açıyor**;
3 koşulana kadar hiçbir şey kazanılmamıştır.

### Ön koşullar

**0. `OWNER_DSN`'i TANIMLA.** Aşağıdaki her komut onu kullanır ve tanımsız bir
kabuk değişkeni `psql ""` demektir — libpq **sessizce** kendi varsayılanlarına
düşer (yerel soket, `$USER`, `$USER` adlı veritabanı) ve komut bambaşka bir yerde
koşar. Bu reponun owner DSN'i **`DATABASE_MIGRATE_URL`**'dir (migration'ın
`tappa_owner` ile bağlandığı değer; `deploy/examples/secret.example.yaml`).

🔴 **PAROLAYI DSN'E KOYMA — bu bölüm KEK'i argv'den yasaklarken owner parolasını
argv'ye koyuyordu, ve tehdit modeli aynı** (`ps` argv'yi görür, kabuk geçmişi de).
`tappa_owner` **yerelde ölçülen** hâliyle `rolsuper=t rolbypassrls=t` ve üretimde de
öyle **olması beklenir** (`10-postgres.yaml` `POSTGRES_USER: tappa_owner` veriyor ve
imajın giriş betiği onu initdb'nin bootstrap süper kullanıcısı yapar) — **üretimde
doğrudan sorulmadı**. Hangi hâlde olursa olsun sızması KEK'ten aşağı kalmaz. Parola **DSN'de değil** — ve bu bölümde **tek bir** parola mekanizması var:
`PGPASSFILE`, adım 2'de `mktemp` ile yaratılan ve `trap` ile silinen **geçici** bir
dosya.

🔴 **`~/.pgpass` YOLU KALDIRILDI, ve sebebi ölçüldü.** Burada eskiden kalıcı
`~/.pgpass`'e yazdıran bir blok vardı ve temizliği literal `HOST` dizesini arayan
bir `sed`'di — gerçek bir dosyada **hiçbir zaman eşleşmez**, yani süper kullanıcı
parolası diskte kalırken runbook *"kalan owner satiri: 0"* basıyordu: koşulsuz bir
no-op, kendi beklentisini doğrulayan. İki mekanizma yerine bir tane olması bu
sınıfı ortadan kaldırıyor — silinecek şey tek bir dosya ve onu `trap` siliyor.

**1. İKİ dosyayı da hazırla: bugünkü KEK ve yeni KEK.** Adım 2 ikisini de ister.

- `umask 077`: yeni dosyalar 0600 doğsun

```bash
umask 077
```

🔴 install -m 600 /dev/null ÖNCE: umask VAR OLAN bir dosyanın iznini değiştirmez ve > bir symlinki
İZLER. /tmp bu makinelerde drwxrwxrwt, ad öngörülebilir ve dosya rotasyondan önce yok — yani ad
önceden yaratılabilir. Ölçüldü: çıplak > ile hedef mode 666 ve symlink KALIYOR; install ile symlink
düz 0600 dosyayla DEĞİŞİYOR. Aynı teknik iki satır aşağıda eski KEK için zaten kullanılıyordu;
yalnız birine uygulanmıştı.

- `openssl rand -base64 32 > /tmp/kek_new.b64`: YENİ KEK — ekrana bakma

```bash
install -m 600 /dev/null /tmp/kek_new.b64
openssl rand -base64 32 > /tmp/kek_new.b64
```

ESKİ KEK: bugün kümede duran değer. Emanetten/sır deposundan al ve dosyaya yaz; terminale yazdırma.
(Küme kopyasının parmak izini operatör adımı 2deki sha256 önekiyle karşılaştırabilirsin — değerin
kendisini basmadan.)

🔴 cat > KULLANMA: etkileşimli tty girdiyi EKRANA YANKILAR. Ama read -rs -p DE KULLANMA — ölçüldü,
zsh'te -p "coprocess'ten oku" demek: read anında 1 dönüyor, && zinciri kırılıyor, printf hiç
koşmuyor (dosya 0 BAYT kalıyor) ve girdi TÜKETİLMEDİĞİ için yapıştırılan KEK bir komut olarak
çalışıp command not found: \<KEK> ile EKRANA BASILIYOR. Aşağıdaki biçim POSIXtir ve bash ile zshte
AYNI davranır (ikisinde de ölçüldü).

- `wc -c < /tmp/kek_old.b64`: 44 (base64) olmalı; 0 ise DUR, yankı riski var

```bash
install -m 600 /dev/null /tmp/kek_old.b64
printf 'ESKI KEK (yapistir, Enter): '
stty -echo; IFS= read -r K; stty echo; printf '\n'
printf '%s' "$K" > /tmp/kek_old.b64
unset K
wc -c < /tmp/kek_old.b64
```
🔴 Yeni KEK de **yeniden üretilemez**. Emanet adımı (operatör adımı 2) eskisi için
neyse yenisi için de odur. **Eski KEK'i adım 3 doğrulanana kadar SİLME.**

**2. Sırlar dosyadan geçsin, komut satırından değil.** `ps` argv'yi görür, kabuk
geçmişi de. ⚠️ **Ve `export` de bedava değil:** ihraç edilen bir değişken o
oturumdaki **her** alt sürece miras geçer ve `/proc/<pid>/environ` üzerinden aynı
UID'ye (ve root'a) okunur. Bitince **temizle** — adım 3'ün sonundaki blok bunu
yapıyor.

**3. Yazma yetkisi.** Migration 00013 `UPDATE(aes_key_ref)`'i `tappa_app`'ten
**kaldırdı**, yani bu prosedür owner ile koşar:
```bash
psql -X "$OWNER_DSN" -At -c "SELECT has_column_privilege(current_user,'tags','aes_key_ref','UPDATE');"
```
🔴 **BU KONTROL YETMEZ VE TEK BAŞINA GÜVEN VERMESİN.** Ölçüldü (2026-08-16,
73 100 satırlık kopya): `NOSUPERUSER NOBYPASSRLS` bir rol bu sorguya **`t`**
cevabı verir ve `tags`'i **hiç göremez** (`FORCE ROW LEVEL SECURITY`, politika
PUBLIC'e yazılı). Asıl kontrol şu — ve aracın ürettiği SQL bunu **kendi içinde
zorunlu kılar**, yani unutulamaz:

```bash
psql -X "$OWNER_DSN" -At -c "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user;"
```

t olmalı. f ise DUR: o oturum parkın tamamını ne görebilir ne yazabilir.

**4. Plaket yüklemeyi DURDUR.** Araç okuma ile yazma arasında parkın büyüklüğünün
değişmediğini doğruluyor; koşu sırasında panelden tek bir plaket eklenirse
rotasyon **fail-closed** iptal olur (ölçüldü: `72860 rows were read but tags holds
72861 … Nothing written`). Doğru davranış ama sürekli yükleme yapılan bir
işletmede rotasyon **hiç tamamlanamaz**. Yükleme penceresini kapat, sonra başla.

**5. Derlemeyi ve geçici dizini SCRIPT yapar — burada elle bir adım YOK.**

🔴 **BU ÖN KOŞUL BİR TUR BOYUNCA ÖLÜ KOD TAŞIDI, ve bir yan etkisi vardı.** Burada
`mktemp -d` + `trap` + `go build -o "$WORK/rotatekek"` yazan bir blok duruyordu ve
`$WORK/rotatekek` **hiçbir yerde çalıştırılmıyordu** — prosedür `scripts/rotate-kek.sh`
çağırıyor, o da kendi `mktemp -d`'sini ve kendi derlemesini yapıyor. Üstelik o
bloğun `trap ... EXIT`'i, adım 2'deki `PGPASSFILE` trap'i tarafından **eziliyordu**
(bash'te ikinci `trap ... EXIT` birincinin yerine geçer), yani operatörün kabuğunda
o geçici dizin **koşulsuz sızıyordu**. Blok kaldırıldı.

Gereken tek şey: **`bash` ve bir Go araç zinciri**. Derlemeyi, `mktemp -d`'yi,
`trap`'i, `ON_ERROR_STOP`'u ve çıkış kodu haritalamasını script yapıyor —
`TestRotateScript_KeepsItsStructuralGuards` bunları tutuyor.

**5a. Aracın koşacağı yer.** `Dockerfile` yalnız `bin/tappa`'yı, migration imajı
yalnız goose'u taşır: **`cmd/rotatekek` hiçbir imajda yok** ve `go run` bir Go
araç zinciri ister. Yani araç bir **checkout + Go** olan bir makinede koşar ve o
makine `OWNER_DSN`'e ulaşabilmelidir.

> ⚠️ **BU KÜMEDE `tappa-postgres:5432`'YE NEREDEN ULAŞILDIĞI ÖLÇÜLMEDİ.**
> `k8s/12-networkpolicy.yaml` 5432'yi yalnız **`tappa` namespace'inde
> `app.kubernetes.io/name: tappa` etiketli pod'lara** açıyor. Operatörün
> dizüstünden `kubectl port-forward` bu kuralın **etrafından dolaşır mı**, bu
> repoda **hiç ölçülmedi**. Ön koşul 0'daki komut cevap vermiyorsa rotasyona
> başlama; önce ulaşımı çöz.

**5b. BU PROSEDÜR RLS'İ ATLAYAN BİR OTURUM AÇAR — kapsamını, ömrünü ve kapanışını
bil.** Ön koşul 3 `rolsuper OR rolbypassrls` istiyor ve üretilen SQL bunu **zorunlu**
kılıyor; yani bu oturum **her tenant politikasını atlar** (§4.5'in kuşağı bu oturumda
yoktur, yalnız kemer — açık `tenant_id` yüklemleri — kalır). Yeni bir delik değil
(migration zaten `tappa_owner` ile koşuyor) ama kural aynı: **aç, koş, kapat.**
Rotasyon bitince `psql` oturumunu kapat, uzun ömürlü bir shell'de tutma, ve bu
oturumu paylaşılan bir makinede açma. Bu oturumun izi `audit_log`'a **düşmez** —
aşağıdaki sayılmış açık.

**5c. KAPININ ÇALIŞMASI, İÇİNDE DUYURU OLAN BİR İMAJIN SEVK EDİLMİŞ OLMASINA
BAĞLI — ve bugün sevk edilmiş DEĞİL.** Ölçüldü (2026-08-17, salt okunur):

```
kubectl -n tappa get deploy tappa -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}{"\n"}{end}'
  -> DATABASE_URL · TAPPA_INVITE_HMAC_KEY · TAPPA_SESSION_HMAC_KEY · TAPPA_TAG_KEK     (DÖRT)
kubectl -n tappa get deploy tappa -o jsonpath='{.spec.template.metadata.annotations}'
  -> (boş: reloader/checksum YOK)
kubectl -n tappa get externalsecrets -n tappa    -> No resources found   (CRD'ler kurulu, nesne yok)
```

Yani **canlı Deployment bu değişikliğin öncesindedir**: beşinci `secretKeyRef`
orada yok ve çalışan ikili `kek_rotation_window=` satırını **yazmıyor**. Adım 1'in
(c) kapısı ancak `20-app.yaml` + yeni imaj sevk edildikten **sonra** anlamlıdır.
Bu sırayla: önce normal deploy (manifest + imaj), **sonra** rotasyon.
⚠️ Bunu böyle yazmanın sebebi: kapı `0 / 0` okursa sebebi *"pencere kapalı"* değil
**"bu ikili o satırı hiç yazmıyor"** olabilir — ve tablo seni zaten durdurur.

**6. SUNUCU TARAFI İFADE LOG'UNU KAPAT — bu adım atlanırsa rotasyon sırların
kopyasını üretir.** Araç 88 hex haneli sarmalı ref'leri SQL olarak yazar; ifade
log'u açıksa bunlar **kalıcı bir dosyaya** düşer. Ölçüldü: bu reponun geliştirme
Postgres'i (`docker-compose.yml`, `-c log_statement=all
-c log_min_duration_statement=0`) 21 satırlık koşularda **105 sarmalı ref
literalini** konteyner log'una yazdı. Üretimin `10-postgres.yaml`'ı `args: []`
ile temiz — ama bu bir **yapılandırma**dır, bir garanti değil, ve §1'in hedefi
olan **managed Postgres**'te sorgu-içgörü boru hatları yaygın olarak açıktır ve
**üçüncü taraf bir log deposuna akar**.

```bash
psql -X "$OWNER_DSN" -At -c "SELECT name||' = '||setting FROM pg_settings WHERE name IN ('log_statement','log_min_duration_statement','log_min_error_statement');"
```

log_statement = none ve log_min_duration_statement = -1 olmalı.

Değilse rotasyondan önce kapat (`ALTER SYSTEM SET log_statement='none'; SELECT
pg_reload_conf();`), sonra eski hâline getir. **`pg_stat_statements` ayrı bir
sorudur:** o ifadeleri **normalize eder**, ama bu araç literaller yazar —
yüklüyse rotasyon sonrası `pg_stat_statements_reset()` çağır.

### Adım 1 — iki KEK'i de yükle, rollout et

`tappa-secrets`'a **`TAPPA_TAG_KEK_PREVIOUS`** ekle (değeri: **bugünkü, sızmış**
KEK) ve `TAPPA_TAG_KEK`'i **yeni** değerle güncelle.

🔴 **SIRRA EKLEMEK YETMEZ — İKİ AYRI SEBEPTEN.**

**(a) Manifest o adı ADIYLA çekmek zorunda.** `20-app.yaml` sırrı **anahtar
anahtar** çekiyor (`envFrom` bilinçli değil: `DATABASE_MIGRATE_URL`'i de sürece
sokardı). Bu dosyanın ilk sürümü *"sırra ekle ve rollout et"* diyordu ve manifestte
o ad **yoktu** → rollout yeşil, değişken **hiç** enjekte edilmez, adım 2 parkı
sunucunun tutmadığı anahtarın altına taşır, her tap **500**, hiç işlem satırı
yazılmadan (§4.6). Bugün `20-app.yaml` onu `optional: true` ile listeliyor.
**⚠️ Ve `optional: true` bir yazım hatasını SESSİZCE YUTAR:** sırra
`TAPPA_TAG_KEK_PREV` yazarsan kubelet pod'u başlatır, değişken enjekte edilmez,
pencere kapalı kalır. Bunu yakalayan tek şey aşağıdaki **(c)** kapısıdır.

⚠️ **`tappa-secrets` bugün ELLE yönetiliyor:** kümede `external-secrets` CRD'leri
**kurulu**, ama `tappa` namespace'inde **hiç `ExternalSecret` yok** (ölçüldü). Yani
"sırra ekle" = `kubectl edit secret` / yeniden apply. **ESO'ya geçilirse adım 1 ve
adım 3 yeniden yazılmalı** (`refreshInterval` da pod'u yeniden başlatmaz —
`examples/externalsecret.example.yaml` bunu zaten söylüyor).

**(b) SIRRI DÜZENLEMEK ÇALIŞAN POD'U YENİDEN BAŞLATMAZ.** `secretKeyRef` ile gelen
env yalnız konteyner **doğarken** okunur, ve bu Deployment'ta `reloader`/`checksum`
annotation'ı **yok** (ölçüldü). Yeniden başlatmayı **sen** yaptırırsın — bu bilgi
repoda `examples/externalsecret.example.yaml`'da zaten yazılıydı, bu bölümde
değildi:

```bash
kubectl -n tappa rollout restart deployment/tappa
kubectl -n tappa rollout status  deployment/tappa --timeout=180s
```
⚠️ `rollout status` **beklenmeden** devam etme: `maxSurge: 1` / `maxUnavailable: 0`
ile yeni pod Ready olduktan sonra bile **eski pod** birkaç saniye servis edebilir ve
onun `TAPPA_TAG_KEK`'i **eskidir**. O saniyelerde adım 2'yi koşarsan eski pod'a düşen
her tap 500 döner.

**(c) KAPI: sürecin KENDİ söylediği POZİTİF olguyu oku.**

- `kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'`: >= 1 OLMALI
- `kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'`: 0 OLMALI

```bash
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'
```

🔴 **BU KAPININ BİÇİMİ BİLİNÇLİ, ÇÜNKÜ ÖNCEKİ BİÇİM YANLIŞTI.** Önceki sürüm
`--since=5m` ile **OPEN satırının YOKLUĞUNU** okuyordu. Uyarı açılışta bir kez,
sonra 15 dakikada bir düşüyor, yani beş dakikalık pencere onu ancak
`(uptime mod 15dk) < 5dk` iken görürdü — **5/15 = %33 doğru**, kalan %67'de
*"pencere kapalı"* derdi. Üstelik bir **yokluk**, şunlardan da doğar: log seviyesi
yükseltilmiş · log dönmüş · yanlış pod'a bakılmış · henüz bakılmamış. *"Kapalı"*
ile *"bilemiyorum"* aynı okumaydı — ve bu kapı, operatörün sızmış KEK'in **emanet
kopyasını imha etmesinden** önceki tek kapıdır.

Artık **bu depodaki ikili** açılışta **iki durumdan tam birini** yazıyor, yani kapı
bir **olgu** okuyor ve üç sonucu birbirinden ayırıyor. ⚠️ **AĞAÇ/KÜME AYRIMI ÖN
KOŞUL 5c'DEKİYLE AYNI VE BURADA DA GEÇERLİ:** *çalışan* ikili bu satırı henüz
yazmıyor (ölçüldü), yani tablo ancak yeni imaj sevk edildikten sonra anlamlıdır —
o zamana kadar okuma **`0/0`** olur ve tablo seni zaten durdurur.

| `open` | `closed` | Anlam |
|---|---|---|
| ≥1 | 0 | pencere **AÇIK** |
| 0 | ≥1 | pencere **KAPALI** |
| 0 | 0 | 🔴 **BİLİNMİYOR — DUR.** Log seviyesi, log dönmesi ya da yanlış pod. Cevap alana kadar ilerleme. |

⚠️ `--since` **kullanma**: satır açılışta düşer ve saatler önce olabilir.
`rollout status` tamamlandıysa `logs deployment/tappa` yeni ReplicaSet'in pod'unu
okur. ⚠️ Kapı `TAPPA_LOG_LEVEL`'a bağlı (`05-config.yaml` = `info`); `error`'a
çekilirse **iki sayı da 0** olur ve tablo seni **durdurur** — güvenli yön.

Uygulama açılışta ikisini de doğrular: `TAPPA_TAG_KEK_PREVIOUS` **isteğe
bağlıdır**, ama **set edildiyse geçerli olmak zorundadır** (32 bayt, base64) ve
`TAPPA_TAG_KEK`'e **eşit olamaz**.

### Adım 2 — parkı yeniden sarmala

🔴 **PROSEDÜRÜN GÖVDESİ ARTIK BU DOSYADA DEĞİL — `scripts/rotate-kek.sh`'TE.**
Sebebi ölçüldü: **bir runbook, belirtilmemiş bir dilde yazılmış bir programdır**
(operatörün kabuğu). Gerçek bir pty'de `zsh -f -i` ile bu bölümün blokları
yapıştırıldığında bir **`#` yorum satırındaki backtick'in içeriği ÇALIŞIYOR** — en
keskin örnekte yorumun uyardığı **saldırgan ikilisinin yolunu yorumun kendisi
koşturuyordu** — ve tek sayıda apostrof taşıyan bir yorum zsh'i `quote>` moduna
sokup **bir sonraki gerçek komutu yutuyordu**. `bash` 3.2'de ikisi de olmuyor,
yani *"bash ve zsh'te aynı"* cümlesi yalnız test edilen blok için doğruydu.

Bir **dosya** bu kategoriyi ortadan kaldırır: kabuk artık **belirtilmiştir**, kimse
yapıştırmaz, ve `ON_ERROR_STOP`/`mktemp`/`trap` operatörün parmaklarında değil
sürecin içindedir. Emsal bu repoda: `scripts/pg-backup.sh`,
`pg-backup-ship.sh`, `pg-restore-verify.sh`.

Parola DSN'de değil, PGPASSFILE'da. Böylece temizlik bir DESEN EŞLEŞTİRMESİ değil, tek bir dosyanın
silinmesi olur (eski sürüm ~/.pgpasste literal "HOST" arıyordu ve gerçek bir dosyada HİÇBİR ZAMAN
eşleşmiyordu — yani temizlik koşulsuz bir no-optu ve yine de "kalan owner satiri: 0" basıyordu).

- `export OWNER_DSN="postgres://tappa_owner@<host>:5432/tappa?sslmode=disable"`: parola YOK
- `export TAPPA_TAG_KEK_FILE=/tmp/kek_old.b64`: bugünkü (sızmış) KEK
- `export TAPPA_TAG_KEK_NEW_FILE=/tmp/kek_new.b64`: yeni KEK
- `./scripts/rotate-kek.sh`: DRY RUN: hiçbir şey uygulamaz, planı ve sayıları gösterir

```bash
umask 077
export PGPASSFILE="$(mktemp)"
trap 'rm -f "$PGPASSFILE"' EXIT INT TERM HUP
printf '%s:5432:tappa:tappa_owner:%s\n' "<host>" "<parola>" > "$PGPASSFILE"
export OWNER_DSN="postgres://tappa_owner@<host>:5432/tappa?sslmode=disable"

export TAPPA_TAG_KEK_FILE=/tmp/kek_old.b64
export TAPPA_TAG_KEK_NEW_FILE=/tmp/kek_new.b64

./scripts/rotate-kek.sh
echo "rc=$?"
```

🔴 YAZMAK ICIN --apply ZORUNLU. Ciplak cagri bir DRY RUN yapar. Bu satir bir tur boyunca "uygular"
yorumuyla ciplak duruyordu ve OLCULDU: rc=0, hicbir sey uygulanmadi, 77.215 satirin tamami ESKI KEK
altinda kaldi -- ve tablo o sifiri "uygulandi" diye okuyordu. Operator Adim 3e gecerse her tap 500
doner.

- `./scripts/rotate-kek.sh --apply`: UYGULAR (geri alinamaz)

```bash
./scripts/rotate-kek.sh --apply
echo "rc=$?"
```

| Kod | Anlamı |
|---|---|
| `0` | Okunan **her** satır yeni KEK altında. **`--apply` ile koşulduysa uygulandı; çıplak çağrıda (DRY RUN) HİÇBİR ŞEY uygulanmadı** — `stderr` hangisi olduğunu yazar. |
| `1` | **REDDETTİ ya da başarısız — hiçbir şey uygulanmadı.** Sebep `stderr`'de. |
| `3` | Uygulandı **ama park tam dönmedi**. **Eski KEK YOK EDİLEMEZ**, adım 3'e geçme. |

Bu üç sayı `TestExitCodes_TheCOMPILEDBinaryReallyDeliversThem` (derlenmiş ikilide)
ve `TestRotateScript_NeverPipesTheToolIntoPsql` (script'in onları maskelememesi)
tarafından tutuluyor.

**Açılamayan satırlar sessizce ATLANMAZ.** Araç sayar, tenant kırılımı ve uzunluk
histogramıyla raporlar ve **varsayılan olarak reddeder**. Devam için sayıyı birebir
beyan et:

- `export TAPPA_ROTATE_ALLOW_UNOPENABLE="<raporun verdiği tam sayi>"`: TIRNAK ŞART

```bash
export TAPPA_ROTATE_ALLOW_UNOPENABLE="<raporun verdiği tam sayi>"
./scripts/rotate-kek.sh --apply; echo "rc=$?"
```

> 🔴 Bu bir **hız kesicidir, KAYIT DEĞİLDİR**. Onay bir ortam değişkeni; sayılar
> yalnız `stderr`'de kalır. **`audit_log` satırı yok, dosyada iz yok, DB'de iz
> yok** — adım 3'ten sonra *"kaç satır sızmış KEK altında kaldı"* sorusunun ürün
> içinde cevabı yoktur; cevap istiyorsan script'i yeniden koş ve çıktısını **sen**
> sakla.

**Üretilen SQL kendi ön ve son koşullarını taşır**, hepsi veritabanında koşar:
oturumun gerçekten RLS'i atladığı (`app.tenant_id` boş **ve**
`rolsuper OR rolbypassrls`) · okunan satır sayısı = `count(*) FROM tags` · eşleşen
satır sayısı = planlanan. **Üçü de sarmallar gönderilmeden ÖNCE** koşar.

🔴 **Ve sarmallar ifade metninde DEĞİL, `COPY` VERİSİNDE gider.** Postgres hata
veren bir ifadenin **tam metnini** log'lar (`log_min_error_statement` varsayılanı
`error`); rotasyon tek bir ~19 MB'lık ifade olduğu sürece **her başarısız koşu**
parkın sarmallarını — yeni KEK altındakiler dahil, yani rotasyondan **sonra da**
hassas olanları — sunucu log'una yazıyordu (ölçüldü: ön koşul 6 sağlanmışken bile
**40 ayrı ref**). Yeni şekilde aynı abort **0 ref** bırakıyor.

### Doğrulama — **ADIM 3'TEN ÖNCE KOŞ**

🔴 **SIRA BİLİNÇLİ VE ÖNCEKİ SÜRÜMDE YANLIŞTI.** Doğrulama adım 3'ten *sonra*
yazılmıştı; tek gerçekten bağımsız kapı olan fiziksel tap, yedek KEK **zaten
düşürüldükten** sonra koşuyordu — yani arıza ancak geri dönüşü olmayan noktadan
sonra görünüyordu.

(a) Parkta eski KEK altında satır kaldı mı — hiçbir şey UYGULAMADAN. İKİ KEK de hâlâ ortamda olmalı
(araç ikisini de ister; biri boşsa exit 1). Açılamayan satırı olan bir parkta
TAPPA_ROTATE_ALLOW_UNOPENABLE de gerekir.

```bash
./scripts/rotate-kek.sh --dry-run; echo "rc=$?   # 0 beklenir, hiçbir şey uygulanmaz"
```

Beklenen: re-sealed under the new KEK ..... 0 ve already under the new KEK = toplam satır sayısı.
(Eskiden burada "re-sealed 0" yazıyordu; araç o dizeyi HİÇ basmıyor, yani literal bir grep her zaman
boş dönerdi.)

(b) Sarmal uzunlukları — 44 dışındaki her sayı bir zarf DEĞİLDİR.

```bash
psql -X "$OWNER_DSN" -c "SELECT octet_length(aes_key_ref) AS len, count(*) FROM tags GROUP BY len ORDER BY count DESC;"
```

(c) GERÇEK BİR TAP — tek uçtan uca kanıt, ve HÂLÂ İKİ KEK YÜKLÜYKEN koşulur. Bir plakete dokun;
kayıt düşmeli. Bu aşamada 500 alıyorsan adım 3e GEÇME. Önce rolloutun oturduğundan emin ol, yoksa
eski poda düşen bir tapi yeni podun kusuru sanarsın:

```bash
kubectl -n tappa rollout status deployment/tappa --timeout=180s
```

### Adım 3 — eski KEK'i düşür (ROTASYON BUDUR)

Yalnız **adım 2 exit 0** verdiyse **ve** doğrulama (a)(b)(c) temizse.
`tappa-secrets`'tan **`TAPPA_TAG_KEK_PREVIOUS`'u kaldır**, sonra:

- `kubectl -n tappa rollout restart deployment/tappa`: sır düzenlemek podu YENİDEN BAŞLATMAZ

```bash
kubectl -n tappa rollout restart deployment/tappa
kubectl -n tappa rollout status  deployment/tappa --timeout=180s
```

**KAPI — yine POZİTİF olgu, aynı tablo:**

- `kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'`: >= 1 OLMALI
- `kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'`: 0 OLMALI

```bash
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'
```

| `closed` | `open` | Ne yap |
|---|---|---|
| ≥1 | 0 | Pencere **KAPANDI**. Devam. |
| 0 | ≥1 | Pencere hâlâ **AÇIK** — sır ya da rollout eksik. **DUR.** |
| 0 | 0 | 🔴 **BİLİNMİYOR — DUR.** Hiçbir şey imha etme. |

🔴 **`0 / 0` OKUMASINDA HİÇBİR ŞEY SİLME.** Önceki sürüm bu noktada sırları
shred'letiyor **ve emanet kopyasını da hedef gösteriyordu**, kapı ise %33 doğruydu:
operatör *"kapandı"* deyip sızmış KEK'in **emanetteki tek kopyasını** yok ederken
süreç o KEK'i kabul etmeye devam edebilirdi — geri alma için gereken kopya da
gitmiş olurdu. §4.7'nin vaadi tam tersine dönüyordu.

YALNIZ yukarıdaki tablo "KAPANDI" dediyse: operatörün kabuğu ve diski temizlenir.

```bash
unset TAPPA_TAG_KEK TAPPA_TAG_KEK_PREVIOUS
```

🔴 "ÜZERİNE YAZARAK SİL" DİYE BİR ADIM YOK — bu satır eskiden öyle diyordu ve YANLIŞTI. Bu platformda
rm -P belgeli bir NO-OPtur ve **0 döner** (man rm: "This flag has no effect."), yani || shred dalına
HİÇ geçilmez; shred de bu makinede yok. Ölçüldü. Dosyalar UNLINK ediliyor, üzerine yazılmıyor.
scripts/rotate-kek.sh kendi geçici dosyaları için bunu sondalayıp söylüyor; burada da doğrusu
yazılıyor. Gerçekten üzerine yazmak istiyorsan: şifreli bir birim ya da RAM disk kullan.

```bash
rm -f /tmp/kek_old.b64 /tmp/kek_new.b64
command -v shred >/dev/null && echo "not: shred var, istersen kullanabilirsin" || \
  echo "not: bu makinede shred YOK ve rm -P bir no-op; dosyalar unlink edildi, uzerine yazilmadi"
```

`$PGPASSFILE`'ı adım 2'nin `trap`'i **henüz silmedi**: etkileşimli bir kabukta `trap … EXIT`
ancak kabuk **kapanınca** koşar, yani pencere açık kaldıkça süper kullanıcı parolası diskte
durur. Şimdi açıkça sil (kalıcı bir parola dosyası bu prosedürde HİÇ yaratılmıyor — tek
mekanizma kuralı; `trap`, pencere bu adımdan önce kapanırsa diye bir yedektir):

```bash
rm -f "$PGPASSFILE"; unset PGPASSFILE
```

⚠️ **ESKİ KEK'İN EMANET KOPYASINI BURADA SİLME.** Adım 4 bitene kadar dur: geri
alma onu gerektirir, ve rotasyon öncesi yedekler hâlâ onun altındadır.

### 🔴 Adım 4 — ROTASYON ÖNCESİ YEDEKLER HÂLÂ SIZMIŞ KEK'İN ALTINDA

**Rotasyon yedekleri döndürmez, ve bunu bilmeden rotasyon tamamlanmış sayılmaz.**
🔴 **BU ADIM KOŞULLUDUR — VE CÜMLE ÜÇ TUR BOYUNCA GENİŞ ZAMANDA KALDI.** Ölçüm
(2026-08-17): `kubectl -n tappa get cronjob` → **`No resources found`**. Yani bu
kümede gönderilen dump **yok**, ve aynı dosya bunu **sınır 1**'de zaten söylüyor.
Mekanizma kipiyle:

> **Yedek CronJob'ı ÇALIŞIYORSA** `50-backup.yaml` her gece tüm veritabanının
> dump'ını küme dışına gönderir ve `BACKUP_RETENTION_DAYS` kadar saklar; aşağıdakiler
> o zaman geçerlidir. **Çalışmıyorsa** (bugünkü ölçüm) imha edilecek dump yoktur ve
> bu adım bugün **boştur** — ama ilk yedek alındığı gün geçerli hâle gelir.

- `kubectl -n tappa get cronjob`: boşsa bu adım bugün için geçersiz

```bash
kubectl -n tappa get cronjob
```

**Yedek çalışıyorken rotasyondan önce alınmış her dump**,
`aes_key_ref`'i **eski KEK altında** taşır — ve içindeki plaket anahtarı, bu
aracın açıkça döndürmediği **aynı** anahtardır. Yani:

> **sızmış KEK + saklanan herhangi bir rotasyon-öncesi dump = parkın tamamının düz
> NTAG anahtarı**, ve bu rotasyondan **sonra** da geçerlidir.

İki seçenekten birini **yap ve hangisini yaptığını yaz**:
- **İmha:** rotasyon öncesi dump'ları sil (hedefte ve varsa yerel kopyalarda), ya da
- **Say:** sızmış KEK'in **`BACKUP_RETENTION_DAYS` boyunca canlı kaldığını** kabul
  et, tarihi not düş ve o tarihe kadar plaket anahtarlarını yanmış say.

Bunu yapmadan *"KEK döndürüldü"* demek, karşılığı olmayan bir garantidir.

### Geri alma

**Adım 3'ten önce** (pencere hâlâ açık): iki değişkeni **yer değiştir** ve boruyu
yeniden koş — araç simetriktir. Okuma yolu ikisini de kabul ettiği için park geri
dönerken de karışık olabilir ve hiçbir tap düşmez
(`TestUnwrapAny_TableOfRotationStates` → *"rollback window, order reversed"*).

**Adım 3'ten sonra**: eski KEK süreçte yok. Geri dönmek onu **yeniden**
`TAPPA_TAG_KEK_PREVIOUS` olarak yüklemeyi gerektirir — **bu yüzden emanette tut.**

### 🔴 MANAGED POSTGRES'TE BU PROSEDÜRÜN YÜRÜTÜLEBİLİR YOLU YOK — SAYILMIŞ LİMİT

Üretilen SQL `rolsuper OR rolbypassrls` **zorunlu** kılıyor (ön koşul 3, ve §4.5
gereği doğru). Ama **`BYPASSRLS`'i yalnız bir süper kullanıcı verebilir** ve §1'in
hedefi olan managed Postgres'te müşteri rolü **süper değildir**. Yani o dünyada
prosedür ön koşul 3'te **durur ve devamı yoktur**.

**Bu bilinçli olarak açık bırakılıyor ve adlandırılıyor**, çünkü bilinen iki çıkış
yolunun ikisi de bu kartta kararı verilmemiş bir şeyi değiştirir:
- `ALTER TABLE tags NO FORCE ROW LEVEL SECURITY` (sahibi RLS'ten muaf tutar) — §4.5'in
  ikinci kemerini rotasyon süresince kaldırır;
- rotasyonu ürünün **kendi** rolüyle koşacak bir yol açmak — 00013'ün
  `UPDATE(aes_key_ref)`'i `tappa_app`'ten almasını geri alır.

Hangisi seçilirse seçilsin bir **ADR** ister. Bugün: **managed Postgres'e taşınırsa
bu prosedür yeniden yazılmalıdır**; bugünkü tek-node kurulumda `tappa_owner` süper
olduğu için yol açıktır.

### Ne zaman DUR

- **Ön koşul 0 cevap vermiyorsa** → dur, `OWNER_DSN` yanlış ya da ulaşım yok.
- **`rolsuper OR rolbypassrls` `f` ise** → dur. O oturum parkı göremez.
- **Adım 1'in log kontrolü 0 sayıyorsa** → dur. Değişken pod'a ulaşmamış.
- **Araç exit 1 verdiyse** → dur. Hiçbir şey yazılmadı; `stderr`'i oku.
- **`psql` ön/son koşulda `ERROR` verdiyse** → dur. Transaction geri alındı. Yeniden
  **oku** ve yeniden koş — eski çıktıyı tekrar uygulama.
- 🔴 **`ERROR: canceling statement due to lock timeout` (SQLSTATE `55P03`) gördüysen
  → BU BEKLENEN BİR SONUÇTUR VE YENİDEN KOŞMAK GÜVENLİDİR.** Üretilen SQL
  `SET LOCAL lock_timeout = '5s'` ile başlar; rotasyonun `UPDATE`'i 5 saniye içinde
  satır kilitlerini alamazsa **kendisi** iptal edilir. Tek transaction olduğu ve
  guard'lar en başta koştuğu için **hiçbir satır yazılmamıştır**; script bunu
  `psql` çıkışını haritalayarak **exit 1** ("refused/failed, nothing applied") diye
  bildirir — **asla exit 3 değil**. Sebep neredeyse her zaman aynı: o anda o
  plaketlere **canlı tap'lar** giriyor (replay guard'ı aynı satırlara `FOR UPDATE`
  alır). Yapılacak: daha sakit bir pencerede yeniden koş. Bu, prosedürün
  **fail-closed** tarafıdır — bkz. `cmd/rotatekek/main.go`'daki `lock_timeout`
  bloğu, ve ⚠️ **oradaki tavanın bir TAP'ı değil, ROTASYONU sınırladığı**
  (backlog T47, 2026-08-19'da düzeltildi: rotasyon tutarken bir tap **10,07 sn**
  bekledi; pozitif kontrol — bekleyen taraf rotasyon — **5,05 sn**'de iptal oldu).
- **Exit 3 aldıysan** → adım 3'e **geçme**.
- **Park büyüklüğü uyuşmuyorsa** → plaket yüklemesi açık kalmış (ön koşul 4).
- **Koşu ortasında öldüyse** → **güvenli**. Araç idempotenttir: yeni KEK'i **önce**
  dener, zaten dönmüş satırları *"already"* sayar. Baştan koş.

### Script kararından sonra: NE YAPISAL OLARAK İMKÂNSIZ, NE HÂLÂ OPERATÖRÜN MAKİNESİNDE

Prosedürün gövdesi `scripts/rotate-kek.sh`'e taşındı. **Taşımak kapatmak değildir**,
o yüzden ikisi ayrı ayrı yazılıyor.

**Yapısal olarak imkânsız hâle gelenler** (operatörün kabuğuna, parmaklarına ya da
hafızasına bağlı değil):

| Ne | Neden artık imkânsız |
|---|---|
| Rotasyonun **gövdesinde** yorumdaki backtick'in çalışması | Gövde artık yapıştırılmıyor: `scripts/rotate-kek.sh`'i **bash bir dosya olarak** yorumluyor (`bash -n` süitte) |
| Kalan **yapıştırılan** bloklarda aynı tehlike | ✅ **YORUM YOK — T136 (OPS-1, 2026-10-18).** Bu bölümde **20 yapıştırılan `bash` bloğu** var (sır girişi, `kubectl` adımları, doğrulama; T136 yorumları bloklardan çıkarırken 13 bloğu yorumların durduğu yerlerden böldü, OPS-1 düzeltme turu adım 3'ün sonuna `$PGPASSFILE`'ı açıkça silen bloğu ekledi). Hiçbirinde ne `#` satırı ne de komut sonunda bir yorum var: açıklama bloğun etrafındaki metinde, satıra özgü not (*"DRY RUN"*, *"UYGULAR"*) bloğun hemen önündeki listede — `TestRunbook_ItsOwnCommandsAreRUNNotSCANNED` iddiayı oradan okuyup komutu **koşturur**. Kural bütün dosyada tek: `TestRunbook_EveryPasteableBlockCarriesNoComment` her `bash` bloğunu (girintili ve `> ` alıntısı içindekiler dahil) yorumsuz tutar ve gerçek `zsh -f -i -n` ile ayrıştırır. Geçmiş: tehlike önce **karakter düzeyinde** kaldırılmıştı (yorumlarda 0 backtick, 0 tek apostrof) ve gerçek `zsh -f -i`'de README şeklindeki backtick'li bir yorum **çalışmıştı** (işaret dosyası yazıldı); o ağ dört dilimi okuyordu, şimdiki dosyanın tamamını. `TestRunbook_PasteableBlocksCarryNoShellHazardInComments` dilimlerin çapalarını ve bu bölümün blok sayısını tutmaya devam eder |
| `ON_ERROR_STOP` unutulması → 40 ref sunucu log'una + `psql rc=0` | Bayrak script'in içinde; `TestRotateScript_AlwaysPassesOnErrorStop` her `psql` çağrısında arıyor |
| Rotasyonun kilit kuyruğunda **süresiz** beklemesi | `SET LOCAL lock_timeout = '5s'` üretilen SQL'in ilk ifadesi; 5 sn'de alamazsa `55P03` ile iptal, tek transaction geri alınır, **0 satır yazılır**, script **exit 1** der. ⚠️ Bu bir **rotasyon** tavanıdır; **TAP tarafında hiçbir tavan yoktur** (`statement_timeout` sunucuda 0 ve hiçbir katmanda set edilmiyor) — ölçüldü, backlog T47 |
| Aracın çıkış kodunun `psql`'inkiyle maskelenmesi | Boru yok; `TOOL_RC` yakalanıp `exit` ediliyor, `TestRotateScript_NeverPipesTheToolIntoPsql` tutuyor |
| Öngörülebilir yola derleme (symlink/dizin/eski ikili) | `mktemp -d` + `chmod 700` + `go build || die` + `-x` kontrolü |
| Geçici dosyaların kalması | `trap cleanup EXIT` — etkileşimli bir yapıştırmada değil, gerçek bir süreçte |
| *"üzerine yazarak sildim"* yalanı | Yetenek **sondalanıyor** (`shred` var mı, `rm -P` gerçekten yazıyor mu) ve yoksa **söyleniyor** |
| `.pgpass` deseninin hiçbir zaman eşleşmemesi | Desen yok: `PGPASSFILE` **tek bir geçici dosya**, temizliği `rm` |

**Hâlâ operatörün makinesine bağlı olanlar — sayılmış limitler:**

1. **Script operatörün makinesinde koşuyor.** `bash` ve bir **Go araç zinciri**
   gerekiyor (`cmd/rotatekek` hiçbir imajda yok). `sh -n` uyumluluğu **iddia
   edilmiyor** — shebang `bash`.
2. **`$TMPDIR` operatörün diski.** Bu platformda `shred` yok ve `rm -P` belgeli bir
   no-op, yani geçici dosyalar **unlink ediliyor, üzerine yazılmıyor**; script bunu
   çalışırken **söylüyor**. Şifreli bir birim ya da RAM disk operatörün kararı.
3. **`OWNER_DSN`'e ulaşım** ölçülmedi (ön koşul 5a; NetworkPolicy 5432'yi yalnız
   etiketli pod'lara açıyor, `port-forward` bu repoda hiç ölçülmedi).
4. **Emanet ve imha** — eski KEK'in kasadaki kopyasını silmek bir insan adımı;
   hiçbir mekanizma onu zorlamıyor ya da engellemiyor.
5. **Adım 1 ve 3'ün `kubectl` adımları** hâlâ elle: sırrı düzenlemek ve
   `rollout restart`. Script bunları **yapmıyor** (bilerek: `tappa-secrets`'a
   dokunan bir otomasyon bu görevin kapsamı dışında ve ayrı bir karar).

### Bu prosedürün NESİ ölçüldü, NESİ ölçülmedi

**Ölçüldü** (2026-08-16, bu reponun geliştirme veritabanının **atılabilir bir
kopyasında** — `CREATE DATABASE … TEMPLATE tappa`, koşuldu, doğrulandı, `DROP`landı):

| Koşu | Sonuç |
|---|---|
| 72 380 satırın tamamı eski KEK altına konup döndürüldü | araç **0,89 sn**, SQL 18,5 MB, `psql` **3,6 sn**, iki son koşul da yeşil |
| dönüşün doğruluğu, **bağımsız** bir uygulamayla | 72 380/72 380 yeni KEK'le açılıyor **ve aynı plaket anahtarını veriyor**; eski KEK'le açılan **0** |
| gerçek karışık park (33 318 açılabilir + 39 062 açılamaz) | varsayılan **ret**; beyanla exit **3**, 33 318 döndü, 39 062 rapor edildi |
| RLS ile daraltılmış okuma + **süper** yazıcı (15 satır) | araç *"15/15 ✓"* dedi, **veritabanı reddetti**, `psql` exit 3, **0 satır yazıldı** |
| 🔴 RLS ile daraltılmış okuma + **daraltılmış yazıcı** (tek `$OWNER_DSN`'in ürettiği kurulum) | **ÖNCE:** araç exit 0, `psql` **COMMIT** exit 0, 73 100'ün **15'i** döndü, doğrulama (a) ve (b) *"başarı"* dedi. **SONRA** (oturum kontrolleri eklendi): `ERROR … app.tenant_id set … Nothing written` ve `ERROR … neither is a superuser nor has BYPASSRLS … Nothing written` |
| `NOSUPERUSER NOBYPASSRLS` bir rolün ön koşul 3'e cevabı | `has_column_privilege(…) = **t**` iken `count(*) FROM tags` = **0** — yani o kontrol tek başına hiçbir şey söylemiyor |
| `20-app.yaml`'ın enjekte ettiği adlar | `TAPPA_TAG_KEK_PREVIOUS` listede **yoktu**; eklendi ve `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` sınıfı kapattı (manifest satırı silinince test **kırmızı**) |
| geliştirme Postgres'inin ifade log'u | `log_statement=all` ile 21 satırlık koşularda **105 sarmalı ref literali** konteyner log'una yazıldı → ön koşul 6 |
| rotasyon sürerken **canlı tap** (§4.4 `last_ctr`) | 12/12 tap başarılı, en kötü bekleme **0,76 sn**; sayaç ilerledi **ve** sarmal değişti — kayıp güncelleme yok |
| yarıda kesilmiş koşunun tekrarı | 72 380 *"already"*, 0 yeniden sarmalama, exit 0 |

**Ölçülmedi, ve bilerek yazılıyor:**

- **Bu prosedür KÜMEYE karşı hiç koşulmadı.** Yerel bir Postgres konteynerine karşı
  koşuldu. Yukarıdaki iddiaların hepsi **mekanizma** hakkındadır (araç, SQL, son
  koşullar, RLS davranışı) — küme hakkında **tek** iddia yoktur, çünkü ölçülmedi.
- **`tappa-postgres:5432`'ye operatörün nereden ulaşacağı** (yukarıdaki uyarı).
- **Üretimde `tappa_owner`'ın süper kullanıcı olup olmadığı doğrudan sorulmadı.**
  `k8s/10-postgres.yaml` `POSTGRES_USER: tappa_owner` veriyor ve `postgres` imajının
  giriş betiği `POSTGRES_USER`'ı **initdb'nin bootstrap süper kullanıcısı** yapar; bu
  bir **mekanizma** iddiasıdır ve yerelde aynı mekanizmayla doğrulandı
  (`rolsuper = t`). Üretimde `SELECT rolsuper` koşulmadı — **ve artık koşulmasına
  gerek yok**: ön koşul 3 operatöre sordurur, ve aracın ürettiği SQL bunu kendi
  içinde **zorunlu** kılar, yani cevap ne olursa olsun yanlış bir oturumda rotasyon
  koşamaz. §1'in hedefi olan **managed Postgres**'te owner'ın süper OLMAMASI
  yaygındır; kontrol tam olarak o dünyada işe yarar.
- **Adım 4 (rotasyon öncesi yedekler) KOŞULMADI.** Bu kümede yedek CronJob'ı yok
  (**sınır 1** — *[yedek kümede HENÜZ YOK]*; ölçüm: `kubectl -n tappa get cronjob`
  → `No resources found`. ⚠️ Burada eskiden **sınır 13** yazıyordu; 13
  bekleme-kabı kuralıdır ve bu ölçümü taşımaz — sınır 8'in kendi uyardığı
  çapraz-atıf kayması), yani imha edilecek bir dump da yok. Adım bir **prosedür** olarak
  yazıldı; ilk gerçek rotasyonda ilk kez koşacak.
- **SAYILMIŞ AÇIK — rotasyonun `audit_log`'da izi YOK.** Park geneli bir
  `aes_key_ref` yeniden yazımı **hiçbir** denetim satırı üretmiyor: araç bir filtre,
  yazan `psql`, ve `audit_log`'a yazan tek yol ürünün kendi kod yollarıdır. Yani
  *"KEK ne zaman, kim tarafından, kaç satırda döndürüldü"* sorusunun ürün içinde
  cevabı yok — cevap operatörün sakladığı çıktıdadır. Aynı şey ön koşul 5b'nin
  BYPASSRLS oturumu için de geçerli.
- **SAYILMIŞ AÇIK — iki-KEK yolunun son kullanma tarihi, sayacı ve göstergesi yok.**
  Süreç pencerenin açık olduğunu söyler, ama *"kaç plaket hâlâ eski KEK'te"*
  sorusunu cevaplamaz; tek cevap tüm parkı yeniden `COPY`layıp araçtan geçirmektir
  (doğrulama adımı (a)). Pencereyi kapatan mekanik bir zamanlayıcı da yok — kapatan
  şey operatörün adım 3'ü koşmasıdır.
- **Ön koşul 6'nın `ALTER SYSTEM` yolu üretimde denenmedi** — geliştirme
  Postgres'inde ifade log'unun sırları yazdığı ölçüldü, kapatma komutu ölçülmedi.

---

## Olay müdahalesi — belirti → sebep

> **Bu bölümdeki satırların ÇOĞU bu kümede fiilen ölçüldü** — ve ölçülmemiş olanlar
> **satırında işaretlidir**. Uydurma senaryo yok; bir belirti burada yoksa, o belirti
> **bu repoda henüz görülmedi** demektir ve teşhis ölçümle başlamalıdır, bu listeyi
> zorlayarak değil.
>
> 🔴 **BU CÜMLE BİR TUR BOYUNCA *"her satır fiilen ölçüldü"* DİYORDU VE ALTINA
> ÖLÇÜLMEMİŞ SATIRLAR EKLENDİ** (bölüm 2'nin Docker Hub satırları: depolar o gün bu kümede
> hiç var olmamıştı, oran limiti yalnız **başlık** olarak ölçüldü; OPS-2'nin Harbor satırları da
> henüz görülmedi ve satırlarında öyle yazıyor). Değişmemiş bir garanti cümlesinin altına yeni
> satır eklemek, garantiyi sessizce yalanlar — bu dosyanın imza kusurunun bir başka yüzü.
>
> 🔴 **VE BÜTÜN BÖLÜM İÇİN GEÇERLİ TEK ÖN KOŞUL: AĞAÇ İLE KÜME AYNI ŞEY DEĞİL.**
> Ağaç Harbor'ı (`registry.everva.com.tr/tappa/...` + `imagePullSecrets: harbor-pull`, OPS-2)
> tarif ediyor; OPS-2'nin ilk deploy'u inene kadar ya da OPS-2 öncesi bir ReplicaSet'e dönülmüşse
> küme Docker Hub'dan kimliksiz koşar. Teşhise başlamadan **kümeye sor**, ağaca değil (ikinci
> komut Secret'ın adını basmalı; boşsa `harbor-pull` bu namespace'te yok):
> ```bash
> kubectl -n tappa get deployment tappa \
>   -o jsonpath='{.spec.template.spec.containers[0].image}{"  ps="}{.spec.template.spec.imagePullSecrets[*].name}{"\n"}'
> kubectl -n tappa get secret harbor-pull --ignore-not-found -o name
> ```
>
> **Önce ölç, sonra dokun.** Kesintinin var olup olmadığının tek dış kanıtı — ilk satır
> *süreç ayakta mı*, ikincisi *DB'ye ulaşıyor mu* sorar:
> ```bash
> curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/healthz
> curl -sS -o /dev/null -w '%{http_code}\n' https://taptime.mt/readyz
> ```
> `/healthz` hiçbir şeye dokunmaz, `/readyz` havuzdan bağlantı alır. **200/200** =
> ürün çalışıyor; **200/503** = süreç ayakta ama veritabanı yok; **ikisi de yok** =
> pod ya da ingress.
>
> 🔴 **ÜÇÜNCÜ BİR SORU VAR — *"ayağa KALKABİLİR miyim"*.** Çalışan pod'un imajı düğümde; yeniden
> başlaması Harbor'a ancak `harbor-pull`'un içeriği pod çekildikten **sonra** değiştiyse gider
> (KEP-2535 doğrulama çekmesi) ya da düğüm imajı düşürdüyse. O durumda k8s-2'nin ayakta ve
> `k8s-pull` robotunun geçerli olması gerekir — sınır **[Harbor k8s-2'ye bağlı]**. Kanıt (komut
> hüküm vermez, kanıt basar): Secret'ın son yazımı ile sunucu pod'larının başlama zamanları.
> ```bash
> kubectl -n tappa get secret harbor-pull -o jsonpath='{range .metadata.managedFields[*]}{.time}{"  by "}{.manager}{"\n"}{end}'
> kubectl -n tappa get pod -l app.kubernetes.io/component=server \
>   -o jsonpath='{range .items[*]}{.status.startTime}{"  "}{.metadata.name}{"\n"}{end}'
> ```
> Secret'ın en yeni yazımından **ÖNCE** başlamış bir pod yeniden başlarken Harbor'a sorar.
> (OPS-2'ye kadar bu kutu `secret/ghcr`'ı anlatıyordu: onu **her deploy** yeniden yazıyordu ve
> token'ı ölüydü, yani böyle bir pod hiç başlayamıyordu — T43. `harbor-pull`'u CI yazmaz; içeriği
> yalnız operatör robotu döndürünce değişir, ve operatör adımı 4 (d) bunu eski robot yaşarken yapar.)

### 1. `connection refused` — bu bir **RST**'tir, "port kapalı" demek DEĞİLDİR

**Belirti.** `goose run: ... dial tcp 10.42.0.x:5432: connect: connection refused`,
ya da `pg_isready ... - no response`. Postgres `1/1 Running` ve loglarında
`listening on IPv4 address "0.0.0.0", port 5432` yazıyor.

> 🔴 **ÖNCE ŞUNU BİL: `no response` ÜÇ FARKLI ARIZAYI AYNI CÜMLEYE BASAR.**
> `pg_isready` (PostgreSQL 17.10) ile ölçüldü:
>
> | gerçekte olan | `pg_isready` çıktısı | exit |
> |---|---|---|
> | kapalı port / RST (**NetworkPolicy reddi buraya düşer**) | `no response` | 2 |
> | paket düşürülüyor (blackhole) | `no response` | 2 |
> | **ad hiç çözülmüyor (DNS / Service / EndpointSlice)** | `no response` | 2 |
> | uid `/etc/passwd`'de yok, `-U` verilmemiş | `no attempt` | 3 |
> | sağlıklı | `accepting connections` | 0 |
>
> Yani **`no response` gördüğünde henüz sebebi bilmiyorsun** — ve üçüncü satır
> teorik değil, *bu deployment'ın kendi ölçülmüş kusuru*: headless Service'in DNS
> kaydı `rollout status` döndükten ~3 sn sonra doğuyor.
>
> **ÖNCE initContainer'ın LOGUNU OKU — ayrımı zaten o yapıyor, üstelik arızalı
> pod'un kendi ağ ad alanından:**
> ```bash
> kubectl -n tappa logs job/tappa-migrate -c wait-for-postgres --tail=20
> kubectl -n tappa logs deployment/tappa  -c wait-for-postgres --tail=20
> ```
> Çıktıyı şöyle oku:
> - `"the NAME RESOLVES to: …"` → soket/politika: bu bölümü oku;
> - `"the NAME … DOES NOT RESOLVE"` → DNS/Service: bu bölüm **SENİN SORUNUN DEĞİL**, 5'e geç.
>
> ⚠️ `container wait-for-postgres is not valid for pod … out of: goose` alıyorsan
> o pod bu initContainer'dan **ÖNCEKİ** bir manifestle doğmuştur — yani FAZ C henüz
> kümeye inmemiştir. Bu bir arıza değil; aşağıdaki ölçüm komutuyla doğrula.
>
> `-c` **zorunlu**: `kubectl logs` init container'ı asla kendiliğinden seçmez.
>
> **Kendin ölçmen gerekirse, `kubectl debug` KULLANMA** — bu namespace `restricted`
> Pod Security ile etiketli ve o yol üç ayrı yerden düşüyor (üçü de ölçüldü):
> varsayılan `--profile=general` → **`Forbidden … violates PodSecurity
> "restricted:latest"`** · `--profile=restricted` **kabul ediliyor ama konteyner
> açılmıyor** → `CreateContainerConfigError: container has runAsNonRoot and image
> will run as root` (`postgres:17-alpine` root çalışır, profil `runAsUser` yazmaz) ·
> ve teşhis edeceğin pod tipik olarak **terminal fazda** (migration pod'u `Failed`),
> orada ephemeral konteyner **hiç koşmuyor** — `ephemeralContainerStatuses` boş
> kalıyor. Bunun yerine uyumlu bir tek-atışlık pod:
> ```bash
> kubectl -n tappa run dnscheck --rm -i --restart=Never \
>   --image=postgres:17-alpine --image-pull-policy=IfNotPresent \
>   --overrides='{"spec":{"securityContext":{"runAsNonRoot":true,"runAsUser":65532,"runAsGroup":65532,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"dnscheck","image":"postgres:17-alpine","imagePullPolicy":"IfNotPresent","command":["sh","-c","getent hosts tappa-postgres || echo NXDOMAIN"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}}]}}'
> ```
> ⚠️ `--rm` pod'u **YALNIZ** kubectl bağlı kalırsa siler. Bu komut tam da pod'ların
> takıldığı bir olayda koşulur, yani Ctrl-C ihtimali yüksek. Her hâlükârda:
> ```bash
> kubectl -n tappa delete pod dnscheck --ignore-not-found
> ```
> ⚠️ **BU SONDA `postgres:17-alpine` İSTİYOR, yani Docker Hub'ın anonim bütçesinden (sınır 12'nin
> eski adı *[Docker Hub anonim çekme bütçesi]*; bölüm 2'nin `429` satırı) bir çekme daha harcayabilir** — ve tam olarak `429` gördüğün bir
> olayda **ters teper**. `imagePullPolicy: IfNotPresent` ve bu imaj Postgres ayaktayken
> düğümde **zaten** duruyor, yani normalde çekme olmaz; ama düğüm imajı düşürmüşse bu
> sonda da açılmaz. Bölüm 2'nin `429` satırını gördüysen önce onu çöz.
> Ölçüldü, iki yönde de: Service yokken `NXDOMAIN`, varken
> `10.43.184.140  tappa-postgres.<ns>.svc.cluster.local …`.
> ⚠️ **Ve çıkan adresi karşılaştır** — `kubectl -n tappa get pod tappa-postgres-0
> -o wide`. Eşleşmiyorsa ad çözülüyor ama **yanlış yeri** gösteriyor (bayat
> EndpointSlice ya da CoreDNS önbelleği; varsayılan TTL **30 sn** — bu ölçüm
> sırasında fiilen görüldü: Service yaratıldıktan hemen sonraki sorgu hâlâ
> `NXDOMAIN` döndü, 20 sn sonra doğru adresi verdi). O durumda bu bölüm değil, 5.
> bölüm geçerlidir.

**Sebep (ad çözülüyorsa).** `12-networkpolicy.yaml` reddediyor. k3s'in denetleyicisi
**REJECT** ediyor, DROP değil, o yüzden zaman aşımı yerine anında ret geliyor — ve bu
*"Postgres henüz hazır değil"* diye okunuyor. **2026-08-15'teki ilk canlı teşhis tam
olarak bu yanlışı yaptı.** En olası alt sebep, çağıran pod'un **çok yeni** olması:
adresi kuralın izin kümesine 0,2–1,0 sn içinde yazılıyor ve o ana kadar reddediliyor.

**Teşhis:**

- `kubectl -n tappa get netpol`: kural duruyor mu
- `kubectl -n tappa get pod <pod> -o jsonpath='{.metadata.labels}'`: etiket doğru mu

```bash
kubectl -n tappa get netpol
kubectl -n tappa get pod <pod> -o jsonpath='{.metadata.labels}'
```
**Çare.** Kuralı **silme** — sildiğin an kümedeki HER namespace'in her pod'u veritabanına
ulaşır (`pg_hba` son satırı `host all all all scram-sha-256`). Çağıran iş yükü
`wait-for-postgres` initContainer'ını taşımıyorsa **onu ekle**
(sınır **[her yeni iş yükü kendi beklemesini taşır]**).
⚠️ **Ağaçtaki manifestler taşıyor; KÜMEDEKİ nesneler henüz taşımıyor.** Bu
runbook'u 04:00'te okuyan kişi **kümeye** bakar, o yüzden iddia değil ölçüm:
```bash
kubectl -n tappa get job/tappa-migrate deployment/tappa \
  -o jsonpath='{range .items[*]}{.metadata.name}{" init="}{.spec.template.spec.initContainers[*].name}{"\n"}{end}'
```
`init=` boş çıkıyorsa bu dosyalar kümeye **henüz inmemiştir** (FAZ C bir deploy
koşusu bekliyor) ve yukarıdaki yarış hâlâ canlıdır.

### 2. `ImagePullBackOff` — sebep mesajdadır, ve hangi registry'den çekildiği KÜMEYE bağlı

> 🔴 **ÖNCE HANGİ DÜNYADASIN (OPS-2, 2026-10-19).** Ağaç Harbor'ı tarif ediyor
> (`registry.everva.com.tr/tappa/...` + `imagePullSecrets: harbor-pull`); OPS-2 öncesi bir
> ReplicaSet'e dönülmüş bir küme Docker Hub'dan kimliksiz çeker. Hangisi olduğunu *"Elle deploy /
> rollback"*'in iki komutu söyler; pod'un kendi imajı `kubectl -n tappa get pod <pod> -o
> jsonpath='{.spec.containers[0].image}'` ile okunur.
> (Bu bölüm OPS-2'ye kadar `secret/ghcr`'ın bayat token'ını — T43, `401` — ve public Docker Hub'ı
> tarif ediyordu; ilki M8-02 FAZ D'de, ikincisi OPS-2'de kalktı.)

**Belirti.** `Failed to pull image ...`, `ErrImagePull`, `ImagePullBackOff`.

**Sebep — `kubectl -n tappa describe pod <pod>` mesajı hangisini söylüyor:**

| mesaj | sebep | çare — **satır bazında; genel bir çare YOK** |
|---|---|---|
| `Unable to retrieve some image pull secrets (harbor-pull)` ve ardından `unauthorized` | `secret/harbor-pull` bu namespace'te **yok** (sandbox'ta ayrıca yaratılır) | Operatör adımı 4 (c)'nin bloğu. Deploy bunu kümeye bir şey uygulamadan yakalar (*"Preflight — the registry pull Secret…"*); buraya elle bir `apply` ya da başka bir namespace getirir |
| `unauthorized` / `401` (Secret **var**) ⚠️ *Harbor dünyasında henüz görülmedi* | `k8s-pull` robotunun kimliği geçersiz — secret'ı yenilendi (Refresh), robot devre dışı bırakıldı ya da silindi, ya da Secret yanlış robotla yaratıldı (robotlar süresizdir, takvimle dolmazlar). Deploy'un ön kontrolü bunu **ölçmez** (Secret'ın varlığını, tipini ve registry'sini ölçer, kimliğin geçerliliğini değil) | Operatör adımı 4 (c) ile Secret'ı geçerli robotla yeniden yarat — blok kimliği bir manifest okuyarak doğrulamadan yazmaz —, sonra takılmış pod'u yeniden yarat (aşağıdaki tablo). Deploy sırasında görüldüyse *"Migrate"* adımı `ImagePullBackOff`'u gördüğü ilk ankette düşmüştür (OPS-2 düzeltme turu 1) ve **sunucu rollout'u yapılmamıştır** — Secret düzeltildikten sonra yeni bir koşu |
| `dial tcp …` / `i/o timeout` / `connection refused` / `no such host` (`registry.everva.com.tr`) | Harbor ya da k8s-2 kapalı — sınır **[Harbor k8s-2'ye bağlı]** | k8s-2'yi / Harbor'ı ayağa kaldır. Çalışan pod etkilenmez (imaj düğümde); yeni imaj o güne dek **çekilemez** — deploy'u ertele |
| `manifest unknown` / `not found` | etiket yok — `:deploy-placeholder` kalmış (dizini toptan `apply` etmişsin), push başarısız olmuş, ya da etiket **son 30 push'tan eski** ve saklama onu silmiş | **Yeni bir deploy koşusu** — doğru etiketi o yazar. Eski bir etikete dönüyorsan Harbor'da (Projects → `tappa` → depo → Artifacts) var olduğunu ölç; manifestleri elle `apply` etme |
| `pull access denied` / `repository does not exist` (`docker.io/atknatk/...`) | OPS-2 öncesi bir ReplicaSet'e dönülmüş ve Docker Hub deposu **private** yapılmış ya da silinmiş | Docker Hub'da depoyu public yap **ya da** o commit'i Harbor üzerinden yeniden sevk et (*"OPS-2 geri dönüş"*) |
| `toomanyrequests` / `429` ⚠️ *bu kümede hiç görülmedi; yalnız başlık ölçüldü* | Docker Hub'ın anonim bütçesi doldu (`x-ratelimit-limit: 100;w=3600`, IP başına) — OPS-2'den sonra **yalnız** `postgres:17-alpine`, `rclone`, `curlimages/curl` ve OPS-2 öncesi imajlar oradan çekilir | **Pencerenin dolmasını bekle** — Deployment pod'u kendiliğinden toparlar. 🔴 **Ama migration `Job`'ı beklemeyle KURTULMAZ:** `activeDeadlineSeconds: 600` + `backoffLimit: 2`, saatlik pencereden **çok önce** kalıcı `Failed` olur → pencere dolduktan sonra yeni bir koşu |

> 🔴 **BU TABLO BİR TUR BOYUNCA *"Çare (her üçünde de) yeni bir deploy koşusudur"* DİYORDU VE
> İLK İKİ SATIRDA GARANTİLİ BAŞARISIZDI** — güvenlik denetimi bunu §4.6'nın altyapı yüzü olarak
> bloklayan saydı: bu bölümün müdahale ettiği hâl uygulama pod'unun **hiç açılmadığı** hâldir ve
> **kaydedilemeyen dokunuş kaybolan kayıttır**. Doğru cevap **satır bazındadır**.

> 🔴 **KAPILAR BİR DEPLOY ANI ÖLÇÜMÜDÜR, SÜREGELEN BİR GARANTİ DEĞİL.** `verify-image.sh --access`
> imajın private ve CI kimliğiyle çekilebilir olduğunu, `pull-secret` `harbor-pull`'un var olduğunu
> **o koşuda** ölçer. Sonradan robot devre dışı bırakılır, proje public yapılır ya da Secret silinir ve
> düğüm imajı düşürürse (kubelet imaj GC'si — bu node'da `imageMinimumGCAge: 2m0s` —, `crictl rmi`,
> düğüm yeniden kurulumu), yeniden başlayan pod bu tablonun bir satırına **elle hiçbir `apply`
> olmadan** düşer. Aranacak şey *"kim elle apply etti"* değil, registry'nin ve Secret'ın **bugünkü**
> hâlidir.

**Her satırda ortak olan tek şey:** takılmış pod'u yeniden yaratmak kubelet'in
büyüyen geri çekilme aralığını sıfırlar — yani **satırın kendi çaresi uygulandıktan
sonra** pod'un dakikalarca beklemesini engeller.

🔴 **AMA HANGİ POD OLDUĞUNA BAK — üç tipin üçü de farklı sonuç veriyor, ve
*"SADECE `ImagePullBackOff` olanı"* bunları AYIRMIYOR:**

| pod | `delete` sonucu |
|---|---|
| `tappa-<hash>-<hash>` (Deployment) | **zararsız** — ReplicaSet yerine yenisini koyar, `maxUnavailable: 0` ile diğer replika servis verir |
| `tappa-migrate-<hash>` (**Job**) | 🔴 **`.status.failed`'ı ARTIRIR.** `backoffLimit: 2` olduğu için **üç silme Job'ı kalıcı `Failed`** yapar; sonra yalnız yeni bir deploy koşusu (Job'ı silip yeniden yaratır) kurtarır |
| `tappa-postgres-0` (**StatefulSet**) | 🔴 **VERİTABANINI YENİDEN BAŞLATIR.** Açık bağlantılar kopar, uygulama `/readyz` 503 verir. Bunu yalnız Postgres'in kendi imajı çekilemiyorsa yap |

- `kubectl -n tappa get pod -o wide`: ÖNCE hangi tip olduğuna bak (ada göre)
- `kubectl -n tappa delete pod <stuck-pod>`: SADECE ImagePullBackOff/ErrImagePull olanı

```bash
kubectl -n tappa get pod -o wide
kubectl -n tappa delete pod <stuck-pod>
```
⚠️ `deploy.yml`'de bunu otomatik yapan bir adım **vardı ve kaldırıldı** — dayandığı
öncül (CI'nın her koşuda yeniden yazdığı, bayat çekme kimliği) artık yok, ve premisi yanlış
olan yıkıcı bir adımı tutmak bu kartın imza kusurudur. Bu işi **elle** ve yukarıdaki tabloya
bakarak yap.

### 3. `pg_isready ... - no attempt` — ağ değil, **kullanıcı adı**

**Belirti.** `tappa-postgres:5432 - no attempt` (çıkış 3), oysa
`getent hosts tappa-postgres` adresi düzgün çözüyor.

**Sebep.** Çağıran pod, imajın `/etc/passwd`'ında **olmayan** bir uid ile koşuyor
(bizim pod'larda 65532) ve libpq varsayılan kullanıcı adını türetemiyor, yani
**soket hiç açılmıyor**. `psql` aynı durumu açıkça söyler:
`local user with ID 65532 does not exist`. **Çare:** komuta `-U tappa_owner -d tappa`
ekle. Bu bir kimlik doğrulaması değildir — `pg_isready` yalnız startup paketi
gönderir, parola sormaz.

### 4. Migration Job'ı `Failed` → **rollout YAPILMAZ**, ve bu doğrudur

**Belirti.** Deploy `::error::the migration Job failed; the deployment is NOT rolled
out` ile durur; canlı pod eski imajla **çalışmaya devam eder**.

**Sebep ve neden böyle.** `deploy.yml` Job'ı bekler ve `failed >= 3` görünce çıkar.
Yeni ikili şema ilerlemeden **servis edilmez**: aksi hâlde henüz var olmayan bir
sütuna yazan bir sürüm 04:00 vardiyasına açılırdı ve §4.6 (*"kayıt asla
kaybolmaz"*) altyapı katmanında karşılıksız kalırdı. **Yani bu bir kesinti
değildir** — ürün ayakta, yalnız yeni sürüm sevk edilmedi. `/healthz` + `/readyz`
ile doğrula, sonra Job'ın logunu oku:

- `kubectl -n tappa logs job/tappa-migrate -c wait-for-postgres`: ulaşabildi mi
- `kubectl -n tappa logs job/tappa-migrate -c goose`: DDL ne dedi

```bash
kubectl -n tappa logs job/tappa-migrate -c wait-for-postgres
kubectl -n tappa logs job/tappa-migrate -c goose
```

⚠️ Birincisi `container wait-for-postgres is not valid for pod … out of: goose` derse o Job
initContainer'sız bir manifestten doğmuştur (FAZ C kümeye inmemiş); yalnız `-c goose` çalışır. Bölüm
1'deki ölçüm komutu hangisi olduğunu söyler.

`wait-for-postgres` başarılı ama `goose` düştüyse sorun **migration'ın kendisidir**,
ağ değil.

### 5. `tappa-postgres` adı çözülmüyor — DNS / Service, NetworkPolicy **değil**

**Belirti.** `wait-for-postgres` logunda *"the NAME tappa-postgres DOES NOT
RESOLVE"*, ya da elle: `getent hosts tappa-postgres` **boş**. `pg_isready` yine
`no response` diyor — bölüm 1'in tablosu bunun neden yanıltıcı olduğunu anlatıyor.

**Sebep.** İki tane, biri ölçülmüş biri yapısal:
- **Geçici (taze kurulum, ölçüldü):** headless Service kendi DNS kaydını **hazır bir
  pod olana kadar yayımlamaz**, ve o kayıt `kubectl rollout status` döndükten
  **~3 sn sonra** doğuyor (taze volume ölçümü: TCP `08:50:35.2` açıldı · kubelet
  `08:50:38` ready · `rollout status` `08:50:39` döndü · ad **`08:50:42`'ye kadar
  NXDOMAIN**). Bu kendiliğinden geçer; `wait-for-postgres` zaten bekler.
- **Kalıcı:** `Service/tappa-postgres` yok, EndpointSlice boş (pod hiç Ready
  olmuyor), ya da `kube-system`'deki CoreDNS cevap vermiyor. 120 sn sonra hâlâ
  görüyorsan **bu budur** — geçici olan bu kadar sürmez.

**Teşhis:**

- `kubectl -n tappa get svc tappa-postgres`: Service var mı
- `kubectl -n tappa get endpointslice`: AYRI çağrı: uç noktalar

```bash
kubectl -n tappa get svc tappa-postgres
kubectl -n tappa get endpointslice
```

⚠️ İkisini tek `get`'e koyma: `get svc tappa-postgres endpointslice` her iki adı da `svc` kaynağında
arar ve `services "endpointslice" not found` + exit 1 verir — yani aradığın arızanın kendisi gibi
görünen sahte bir hata.

- `kubectl -n tappa get pod tappa-postgres-0`: Ready mi (headless kaydın şartı)
- `kubectl -n kube-system get pod -l k8s-app=kube-dns`: CoreDNS ayakta mı

```bash
kubectl -n tappa get pod tappa-postgres-0
kubectl -n kube-system get pod -l k8s-app=kube-dns
```
**Çare.** `12-networkpolicy.yaml`'a **dokunma** — bu bölümdeki hiçbir arıza onunla
ilgili değildir ve kuralı silmek yalnız veritabanını kümeye açar.

### Sağlıklı bir taze kurulum ne kadar sürer (ölçüldü, 2026-08-16)

Sayılar **`apply`'dan itibaren kümülatiftir**, adım süresi değil. **Üç ayrı taze
koşu**, çünkü tek bir ölçüm bir aralık değildir. ⚠️ Bu cümle bir tur boyunca *"iki
ayrı"* dedi, tablo ise üç sütun taşıyordu — metin ile tablonun çelişmesi bu bölümde
**ikinci kez** oldu; sayıyı iki yerde birden yazmak yerine tabloyu tek kaynak say.

| Kilometre taşı | koşu 1 | koşu 2 | koşu 3 |
|---|---|---|---|
| `rollout status statefulset/tappa-postgres` döner | 14 sn | 17 sn | 17 sn |
| migration Job `Complete` | 21 sn | 25 sn | 26 sn |
| sunucu Deployment `Ready`, `RESTARTS=0` | 30 sn | 33 sn | 33 sn |

Boş namespace'ten çalışan ürüne **~30–35 saniye**, elle hiçbir müdahale olmadan
(`kubectl delete pod` yok, elle NetworkPolicy silme yok). Bir deploy bunun **çok**
ötesine geçiyorsa yukarıdaki beş bölümden biri işliyordur.

⚠️ Bu sayılar **boş bir node ve önbellekli imajlarla** ölçüldü; ilk gerçek çekme,
yüklü bir node ve `local-path` üzerinde initdb bunları büyütür. Mutlak bir bütçe
değil, *"bir şey takıldı mı"* sorusuna kaba bir cevaptır.

---

## Gözlemlenebilirlik — M8-03

> **Bu bölümün her sayısı bu kümede ölçüldü (2026-08-19, salt okuma), ve ölçülemeyen
> her şey satırında `DOĞRULANAMADI` diye işaretli.** Ölçüm komutları satırların
> yanında; hiçbiri kümeyi değiştirmez.

### Log nereden çıkar, nereye gider

| Katman | Ne | Ölçüm |
|---|---|---|
| Süreç | `log/slog`, stdout | `TAPPA_LOG_FORMAT` — `text` (varsayılan) veya `json`; ConfigMap prod'da `json` verir |
| Node | kubelet, `/var/log/pods/<ns>_<pod>_<uid>/<container>/*.log` | `kubectl logs` bunu okur; yol, otel ajanının `varlog -> /var/log` hostPath'i ve `include: /var/log/pods/*/*/*.log` globuyla doğrulandı |
| Toplayıcı | **SigNoz otel ajanı bu log'ları TOPLUYOR** | `kubectl -n signoz get ds k8s-infra-otel-agent`; `filelog/k8s` alıcısının `exclude` listesi `tappa` namespace'ini **saymıyor** |
| Depo | ClickHouse, **aynı node** | ajanın dışa aktarıcısı `http://signoz-otel-collector.signoz.svc.cluster.local:4318` — küme içi, dışarı çıkış yok |

**Yani prod log'unun İKİ kopyası var** ve ikisi de aynı fiziksel makinede.

🔴 **VE TOPLAYICIYA GİDEN ŞEY BİR SÜREÇ DEĞİL, ÜÇ KONTEYNERDİR — bu tablo bir tur
boyunca yalnız uygulamayı sayıyordu.** Ajanın `filelog` globu
`/var/log/pods/*/*/*.log`'dur, yani namespace'teki **her** konteyner. Ölçüldü
(2026-08-19, salt okuma):

| Konteyner | Satır | İçinde ne var |
|---|---|---|
| `tappa` (uygulama) | 3 | `log/slog`, yukarıdaki tablonun konusu |
| `tappa-postgres-0` → `postgres` | 92 | 4'ü `ERROR`/`STATEMENT`. 🔴 **Postgres bir hata satırında İFADENİN METNİNİ basar**, ve tap insert'i `gps_lat`/`gps_lng` taşır |
| `tappa-migrate-*` → `goose` | 1 | migration çıktısı |

⚠️ **BU BUGÜN BİR SIZINTI DEĞİL, SAYILMAMIŞ BİR KAPSAM.** Ölçüldü: bugünkü
postgres log'unda `gps_lat`/`gps_lng` geçen satır sayısı **0** — çünkü henüz
gerçek tap yok ve `log_statement=none` / `log_min_duration_statement=-1`
ayarları yürürlükte (KEK bölümündeki ön koşul bunu zaten kontrol ediyor). Ama
**§4.7'nin mekanizması buraya uzanmıyor**: `scripts/redline-check.sh` Go kaynağını
tarar, `geo.Point.LogValue` Go tipini kapatır; ikisi de Postgres'in kendi hata
satırına dokunamaz. Yani başarısız bir tap insert'i, veritabanı ayarları
değişirse, koordinatı **uygulamanın hiç görmediği bir yoldan** aynı toplayıcıya
yazabilir. **Kapatmak bu kartın işi değil — sayılıyor, susulmuyor** (M8-04
kapsamı; `log_statement`/`log_min_duration_statement`'ın manifestle sabitlenmesi
ve pilot öncesi doğrulanması).

node ve bölge

```bash
kubectl get nodes -o wide
```

```text
-> k8s-1.fsn1.private  Ready  control-plane  v1.35.4+k3s1  10.0.0.10  Ubuntu 24.04.4
```

⚠️ topology.kubernetes.io/region ETİKETİ YOK (ölçüldü: boş). Bölge kanıtı node ADI (fsn1 = Hetzner
Falkenstein, Almanya) ve M8-02 kartında kaydedilen 144.76.158.60 adresidir — bir etiket değil.

```bash
kubectl get nodes -o jsonpath='{.items[0].metadata.labels}'
```

toplayıcı gerçekten bizi okuyor mu

```bash
kubectl -n signoz get cm k8s-infra-otel-agent -o jsonpath='{.data.otel-agent-config\.yaml}' \
  | grep -A 20 'filelog'
kubectl -n signoz get ds k8s-infra-otel-agent \
  -o jsonpath='{range .spec.template.spec.containers[*].env[*]}{.name}{"="}{.value}{"\n"}{end}' \
  | grep OTLP
```

**AB kriteri:** node **fsn1 (Falkenstein, Almanya)**, toplayıcı ve ClickHouse
**aynı node**. Log AB'den çıkmıyor. ✅

### 🔴 SAKLAMA SÜRESİ — ÖLÇÜLDÜ, VE BUGÜN BİR SÜRE DEĞİL, BİR BOYUT

```bash
kubectl get --raw "/api/v1/nodes/k8s-1.fsn1.private/proxy/configz" | tr ',' '\n' \
  | grep -i containerLog
```

```text
-> "containerLogMaxSize":"10Mi"
   "containerLogMaxFiles":5
   "containerLogMaxWorkers":1
   "containerLogMonitorInterval":"10s"
```

**Node kopyası: konteyner başına 10 MiB × 5 dosya = 50 MiB, sonra en eskisi silinir.**
Bu bir **boyut** sınırıdır; hiçbir katmanda **zaman** sınırı yoktur.

Bugünkü hacimle bu ne demek:

```bash
kubectl -n tappa logs $(kubectl -n tappa get pod -l app.kubernetes.io/component=server \
  -o jsonpath='{.items[0].metadata.name}') | wc -lc
```

⚠️ BURAYA BEKLENEN BİR ÇIKTI YAZILMIYOR, BİLEREK. Bu sayı pod her yeniden başladığında sıfırlanır ve
pod yaşıyla birlikte büyür; bir tur onu "3 satır, 489 bayt" diye dondurdu, sonraki ölçüm 4 satır /
557 bayt verdi. Yapıştırıp kendi çıktını oku; karşılaştırılacak bir referans YOK.

🔴 **VE BU ÖLÇÜM SEVK EDİLEN İKİLİYE AİT DEĞİL — bir tur boyunca öyleymiş gibi
okundu.** Kümede duran pod `sha-97537336af03` imajını koşuyor, yani **M8-03
öncesi** ikiliyi; ölçüldü: o pod'un log'unda `http.request` kaydı sayısı **0**.
Yani 489 bayt, **erişim kaydı olmayan** bir sürümün rakamıdır ve bu koddaki hacim
hakkında hiçbir şey söylemez. Bir tur *"489 bayt / 19 saat hızında 50 MiB asla
dolmaz, fiilî saklama süresi sınırsıza yakındır"* yazdı; **o cümle yanlıştı.**

**Doğru hesap, ölçülmüş parçalarla:**

| Ne | Değer | Nasıl ölçüldü |
|---|---|---|
| Bir `http.request` kaydının boyutu | **195–200 bayt** (JSON, üretim şeklinde bir `request_id` ile 200) | `internal/httpx` içinde gerçek `slog.JSONHandler` çıktısı sayıldı |
| 50 MiB kaç kayıt alır | **≈ 262.000 yanıt** | 52.428.800 / 200 |
| Sağlık sondalarının katkısı | 🔴 **0** (kararlı durumda) | Sondalar tasarlandığı gibi cevap verirken kayıt yazılmıyor — üstteki bölüm |
| …düzeltmeden ÖNCE olsaydı | 25.920 istek/gün × 197 B = **5,1 MB/gün** → 50 MiB **≈ 10,3 günde** dolardı, ve bu hacmin **~%100'ü sonda** olurdu | `deploy/k8s/20-app.yaml`: `/readyz` 5 sn, `/healthz` 10 sn |
| Gerçek pilot hacmi | **DOĞRULANAMADI** | Bir tap'ın kaç HTTP yanıtı ürettiği (sayfa + statik varlıklar + `POST /api/checkin`) tarayıcıya bağlıdır ve bu turda ölçülmedi |

**Yani "sınırsıza yakın" bir süre değil, bir SAYIMDIR:** node kopyası
konteyner başına ~262.000 erişim kaydından sonra en eskisini atar. Sonda trafiği
artık o sayacı ilerletmiyor; ilerleten şey **gerçek kullanıcı trafiğidir** ve
onun hızı pilot başlayana kadar bilinmiyor. **Bir sayıyı buraya dondurmak yerine
ölçüm komutu yazılıyor** — pilot açıldıktan sonra koş:

Erişim kaydı hizi: iki olcum arasindaki fark / gecen sure. (Once bir kez, 10 dakika sonra bir kez;
ikisinin farki gunluk hizi verir.)

```bash
POD=$(kubectl -n tappa get pod -l app.kubernetes.io/component=server \
  -o jsonpath='{.items[0].metadata.name}')
kubectl -n tappa logs "$POD" | wc -lc
kubectl -n tappa logs "$POD" | grep -c http.request
```

50 MiB / (kayit boyutu) = kac yanit sigar; hiz x gun = ne zaman dolar.

Ve GDPR açısından önemli olan yarı bu **değil**, şu: log satırları IP ve çalışan
id'si taşır (§4.2, §4.7), Q13'ün silme akışı `employees` üzerinde bir UPDATE'tir
ve **log'a hiç ulaşmaz** — dolayısıyla saklama süresi ne kadar uzunsa o kadar
kötüdür, ne kadar bilinmezse o kadar kötüdür.

⚠️ **VE BU SINIRSIZLIK YANILTICI, ÇÜNKÜ İKİNCİ BİR SİLİCİ VAR: DEPLOY.** Pod
silindiğinde kubelet `/var/log/pods/<uid>` dizinini kaldırır, yani her rollout
öncekinin log'unu atar. Ölçüldü:

- `kubectl -n tappa get rs`: 7 ReplicaSet = 7 rollout

```bash
kubectl -n tappa get rs
kubectl -n tappa get pods -o name
```

```text
-> pod/tappa-6fd5489dcb-q5b4l      <- UYGULAMA: bundan YALNIZ BİRİ var
   pod/tappa-migrate-rq76l          (tamamlanmış Job)
   pod/tappa-postgres-0             (StatefulSet)
```

⚠️ ÇIKTI ÜÇ SATIR. Bir tur burada "(YALNIZ BİRİ)" yazıp tek satır gösteriyordu; "yalnız biri" olan
şey UYGULAMA POD SATIRIDIR, komutun çıktısı değil. Diğer ikisi aşağıdaki §4.7 kapsam notunun konusu.

**Son yedi deploy'un altısının log'u node'da yok.** Yani node kopyası için gerçek
saklama süresi *"50 MiB dolana kadar"* değil, **"bir sonraki deploy'a kadar"**dır —
ki bu bugün ~günlerdir. İki sınırın **küçüğü** geçerlidir.

### Politika — ve neyin operatör eylemi olduğu

1. **Node kopyası: 50 MiB / konteyner, üst sınır bir sonraki deploy.** Bugünkü
   ayar budur ve **manifestle sabitlenemez** — `containerLogMaxSize` /
   `containerLogMaxFiles` **kubelet** ayarlarıdır, pod'un değil. Değiştirmek
   **operatör eylemi**dir:
   **OPERATÖR EYLEMİ — `log-retention-kubelet`:** `/etc/systemd/system/k3s.service`
   içindeki k3s server satırına
   `--kubelet-arg=container-log-max-size=10Mi --kubelet-arg=container-log-max-files=5`
   (veya istenen değerler) eklenir, `systemctl daemon-reload && systemctl restart k3s`.
   ⚠️ Bu **node'daki her namespace'i** etkiler; Tappa'ya özel değildir.
2. **Toplayıcı kopyası (SigNoz/ClickHouse): saklama süresi DOĞRULANAMADI.** SigNoz'un
   TTL'i ClickHouse'ta durur ve okumak için ya ClickHouse'a `exec` ya SigNoz API'si
   gerekir; bu turda **yalnız `get`/`describe`/`logs`/`--raw` kullanıldı**, ikisi de
   yapılmadı. **Varsayılana güvenilmedi ve buraya bir sayı yazılmadı.**
   **OPERATÖR EYLEMİ — `log-retention-signoz`:** SigNoz arayüzünde
   *Settings → Retention* açılır, **logs** için bir süre okunur/ayarlanır ve **bu
   satıra yazılır**. Sahibi: kümeyi işleten operatör (Tappa değil — SigNoz kurulumu
   başka bir ürünün altyapısı ve `tappa` namespace'i ona **rıza vermeden** dahil
   edilmiş durumda).
3. **🔴 M8-06 PİLOT KAPISI.** Yukarıdaki 2. madde kapanmadan gerçek çalışan tap
   atmamalı: Tappa'nın kendi kayıtları için `TAPPA_RETENTION_YEARS` bir beyan taşıyor
   (Q13), ama **log'lar o beyanın kapsamında değil** ve bugün süreleri bilinmiyor.
   Bilinmeyen bir saklama süresi, GDPR Art. 13 metninde verilen sayıyı yalanlayabilir.

### 🔴 M8-03 — UYARI KURALLARI

> **TESLİMAT VAR, AMA KÜMENİN İÇİNDEN BAŞLIYOR — VE ONU NEYİN SUSTURDUĞU AŞAĞIDA SAYILI.**
> (T118, 2026-10-11; `Q28 (a)`, kullanıcı kararı: uyarılar SigNoz'dan.) Aşağıdaki tablonun
> yedi kuralı kümedeki SigNoz'da (v0.131, `signoz.everva.com.tr`, ArgoCD `infra-signoz`)
> **ClickHouse-SQL eşik kuralı** olarak kurulu. Kaynakları bu repoda:
> **`deploy/observability/signoz-alert-rules.json`** (sırsız, id'siz; yeniden kurulum
> operatör adımı 10 (f)). Zincir: kural → SigNoz webhook kanalı `tappa-healthchecks` →
> healthchecks.io kontrolü **`tappa-signals`** (gövdede `firing` = `down`, `resolved` =
> `up`) → Telegram + e-posta.
> **Ölçülen (orkestratör, 2026-10-11):** yedi sorgu ClickHouse'ta son 24 saate karşı koştu
> ve yedisi geçerli; filtrelerin dayandığı alanların gerçekten dolu geldiği ayrıca görüldü
> (`level` {`INFO`, `ERROR`} · `operator_surface` {`off`, `configured`} · `verdict` {`flag`,
> `reject`} · `ctr_gap` her `tap.decision`'da); yedi kural `inactive` (hatasız
> değerlendiriliyor); uçtan uca: SigNoz'un test uyarısı bir sonda kontrolünde `down` üretti.
> **Pinli:** dosyadaki her kuralın gövde alanları ve `msg` değerleri koddaki sabitlerle
> (`TestSignozRules_FilterOnThePinnedNames`, `TestObservability_AlertSignalNames`'in
> eşlemesinden), her kural bu tablonun aynı numaralı satırıyla — filtresi ve eşiği
> satırın yazdığıyla aynı (`TestSignozRules_OneRuleForEachRunbookRow`), her sorgu
> `tappa` namespace'i ve konteyneriyle sınırlı (`TestSignozRules_EveryQueryIsScopedToTappa`);
> her kuralın penceresi, frekansı, `op`/`target`/`matchType`'ı ve `app`/`severity`
> etiketleri kural başına yazılı değerlerine, pencere ayrıca satırın *"N dk'lık
> pencere"*sine bağlı (`TestSignozRules_ConditionsAndLabelsArePinned`, T122 — o zamana
> kadar bu alanları hiçbir test okumuyordu); dosyada adres ve healthchecks.io anahtar
> kelimesi yok (`TestSignozRules_CarryNoAddressAndNoDeliveryKeyword`).
>
> **T122 (2026-10-13) — 8. kural dosyada, tabloda ve kümede.** Orkestratör `548f194`'ün
> deploy'undan sonra kurdu: SigNoz'da sekiz `tappa` kuralı, 8.si `inactive`, kanal
> `tappa-healthchecks` → `tappa-signals`. Sorgusu ClickHouse'a karşı bir kez koşuldu
> (`/api/v5/query_range`, son 6 saat): HTTP 200, 0 seri — kontrol olarak koşulan 3. kuralla
> aynı; yani sorgu **hatasız**, olay henüz **yok**. Uçtan uca ateşleme (olay → bildirim)
> 8. kural için **ölçülmedi**. ⚠️ Kümede 1–7 zaten varken dosyanın tamamını adım 10 (f) (2)'deki döngüyle
> yeniden göndermek 1–7'nin **ikinci kopyalarını** doğurur — yalnız 8. kural gönderilir
> (dizinin sekizinci elemanı). Şiddeti `warning`: hiçbir dokunuş sessizce onaylanmaz —
> kayıt çoğunlukla `flag` olarak yazılır ve müdür görür (§4.6); veritabanı bütünüyle
> erişilemiyorsa kayıt da düşer ama dokunuş kişiye görünür biçimde reddedilir ve o
> arızayı `critical` olan 6. kural taşır. Bedeli müdür kuyruğudur. Bu dosyada
> `critical`, sunucunun bozuk olduğuna (5, 6) ya da bir güvenlik olayına (3)
> ayrılmıştır.
>
> ❌ **Sayılı sınırlar — kurulu kural tablodan DARDIR ve zincirin kör noktaları var:**
> - **Basitleştirmeler.** 1. kural *"≥ 5 reject **ve** oran > %10"*u tek ifadede tutar
>   (5'in altında oran 0 sayılır). 2. kural *"toplam > 20 **veya** tek `matched_sid` > 10"*u
>   tek bir 0/1 değerine indirir — hangi kolun ateşlediği bildirimde görünmez. 4. kural
>   `ctr_gap > 10`; *"> 50 acil"* ayrımı yalnız açıklamada — ayrı kural yok. 5. kuralın
>   *"toplam isteğin %1'i"* kolu **kurulmadı**, yalnız *"5 dk'da > 5 olay"*. 7. kural yalnız
>   `unavailable`'ı okur; `off` bilinçli kapatılmış yüzeydir, uyarı değil.
> - **Pencereler kayan değil, saate hizalı kovalardır** (`toStartOfInterval`; SQL'den
>   okundu, ölçülmedi). Değerlendirme penceresi genellikle iki kovaya bölünür, yani eşiğin
>   hemen üstünde bir patlama sınırdan ikiye ayrılıp **ateşlemeyebilir** — 1., 2. ve 5.
>   kuralda. Eşiği `0` olan 3., 4., 6., 7. ve 8. kuralı etkilemez (tek satır yeter).
> - **SigNoz'un kendi e-postası kapalı** — tek teslim yolu healthchecks.io (sınır 33 (a):
>   tek sağlayıcı).
> - **SigNoz kümede.** Küme ya da node ölürse hepsi **birlikte susar** — o durumu
>   `tappa-uptime` (heartbeat, adım 10) yakalar. Ama SigNoz'un **tek başına** ölmesi
>   (pod'u, ClickHouse'u, log toplayan ajan) **hiçbir yerde uyarı üretmez**:
>   `tappa-signals`'ın toleransı 365 gündür, yani bir ölü adam anahtarı değildir (sınır 25).
> - **Tek kontrol, bütün kurallar.** Herhangi bir kuralın `resolved`'ı kontrolü `up`'a çeker —
>   başka bir kural hâlâ ateşteyken bile; ve kontrol zaten `down` iken ikinci bir kuralın
>   ateşlenmesi **yeni bildirim üretmez** (healthchecks.io durum **değişiminde** bildirir).
>   Bu tasarımın sonucudur, **ölçülmedi**. Hangi kuralların ateşte olduğunu SigNoz →
>   *Alerts* söyler.
> - **Log saklama `Q28 (b)` hâlâ açık** (sınır 24, operatör eylemi `log-retention-signoz`)
>   — bir uyarıyı araştırmak için log'un hâlâ orada olması gerekir.
>
> **Sahibi:** kümeyi işleten operatör (SigNoz ve healthchecks.io hesabı). **Neye bağlı:**
> `Q28 (b)` — log saklama süresi (**saklama**) · operatör eylemi `log-retention-signoz`
> (SigNoz'un logları gerçekten tuttuğu süre, `Q28 (b)`'nin ölçülmemiş yarısı).
>
> ⚠️ **BURADA BİR TUR BOYUNCA `Q12` YAZIYORDU VE ÜÇ YERDE YANLIŞTI.** `Q12`
> ***barındırma*** sorusudur (VPS sağlayıcı, managed Postgres, AB bölgesi, yedek
> politikası); `open-questions.md`'de *"uyarı"*, *"alarm"*, *"bildirim"* geçen tek
> satır yoktu — yani atıf, **var olmayan bir kararı** devrediyordu. Soru M8-03'ün
> 2. tur denetiminde açıldı: **`Q28` — uyarı hedefi ve log saklama süresi.**
> ⚠️ `Q12` **saklama tarafında hâlâ kısmen ilgili**, ama sahibi değil: barındırma
> kararı log'ların **fiziksel olarak nerede durduğunu** belirler (bugün k3s node'u +
> aynı node'daki SigNoz), `Q28 (b)` ise **ne kadar** durduklarını. Sağlayıcı
> değişirse node kopyasının rotasyon ayarı ve toplayıcının varlığı **birlikte**
> değişir — bu yüzden ikisi de anılıyor, ve karıştırılmıyor.
>
> ⚠️ **BU KURALLARIN HEPSİ `TAPPA_LOG_FORMAT=json` VARSAYAR.** `text` ile gövde
> toplayıcıya **tek bir opak dize** olarak varır — ajanın `filelog` alıcısı yalnız
> `container` operatörünü koşuyor, logfmt ayrıştırıcısı **yok** (ölçüldü). O
> durumda aşağıdaki alan filtreleri **hiçbir satırla eşleşmez**, ki bu ekranda
> *"hiç reject yok, hiç 5xx yok"* diye görünür — sessiz ölüm.

> ⚠️ **T116 (2026-10-09) — bu tablonun DIŞINDA iki sinyal de teslim ediliyor:** halka
> açık `/healthz`'in 200 dönmemesi (kümenin ölümü dahil — o zaman hiç sinyal gelmez) ve
> gecelik yedeğin sonucu, healthchecks.io'nun ölü adam anahtarı üzerinden (operatör adımı
> 10, sınır 33). ~~Bu tablonun yedi kuralı için *"teslimat kanalı yok"* cümlesi aynen
> geçerlidir.~~ → **T118 (2026-10-11): yedi kural da aynı sağlayıcıya, `tappa-signals`
> üzerinden gidiyor** (yukarıdaki kutu).

**Sekiz sinyal**, beş olaydan ve bir açılış satırından hesaplanır (aşağıdaki tablo sekiz
satırdır; bir tur boyunca burada *"beş"* yazıyordu ve 6. kural eklendikten sonra belge
kendisiyle çelişiyordu; 7. kural M10 OP-8'de eklendi — sınır 32; 8. kural T122'de):

| # | Sinyal | Olay (`msg`) | Filtre | Eşik / pencere | Ne anlama gelir | İlk bakılacak yer |
|---|---|---|---|---|---|---|
| 1 | **`reject` oranı sıçraması** | `tap.decision` | `verdict = "reject"` | 15 dk'lık pencerede `reject` / toplam **> %10** **ve** en az 5 reject | Ya bir plaket `lost`/`retired`/`unassigned` (§5 satır 1), ya SUN doğrulaması düşüyor (satır 2), ya bir hesap deaktive (satır 4). Üçü de farklı arıza. | `matched_sid` alanı hangi satır olduğunu söyler — `sys:tag-not-active`, `sys:sun-invalid`, `sys:employee-deactivated` |
| 2 | **`flag` kuyruğu birikmesi** | `tap.decision` | `verdict = "flag"` | 60 dk'lık pencerede **> 20** flag, **veya** aynı `matched_sid` ile **> 10** | Kanıt yetersiz kalıyor ve §4.6 gereği kayıtlar müdür kuyruğuna düşüyor. En sık sebebi bir lokasyonun statik IP'sinin değişmesi. | `matched_sid`; sonra panelin onay kuyruğu ve `locations.static_ips` |
| 3 | **güvenlik olayı — İKİ AYRI SEBEP** | `tap.security_alert` | olayın kendisi | **≥ 1** olay = uyarı. Pencere yok. | 🔴 **Bu olay İKİ yoldan doğar ve ikisi farklı işler.** (a) §5 satır 4 — canlı bir oturum, **kapatılmış bir hesapla** plakete dokundu (`matched_sid = sys:employee-deactivated`): ya ayrılan biri telefonunu kullanıyor, ya bir oturum çalınmış. (b) §5 satır 1 — **kayıp bildirilen bir plakete** dokunuldu (`matched_sid = sys:tag-not-active`, tag durumu `lost`): plaket duvardan sökülmüş ya da kopyalanmaya çalışılıyor. ⚠️ `retired` ve `unassigned` **uyarı üretmez** — o ikisi rutin yaşam döngüsü. | **Önce `matched_sid`'e bak, hangi yol olduğunu O söyler.** (a) için `employee_id`; (b) için `tag_uid` ve `plaques` ekranındaki plaket geçmişi. Her iki durumda `audit_log`'da aynı adla kanıt satırı var (`tap.security_alert`) |
| 4 | **şüpheli `ctr` sıçraması** | `tap.decision` | `ctr_gap > 10` | tek olay bile bakılmaya değer; **> 50** acil | Çip, sunucunun görmediği kadar okundu — URL biriktirmenin (A1) tek gözlemlenebilir izi (Q21). | `tag_uid` yok bu olayda: `matched_sid = base:ctr-gap-review` ile `transactions` satırını bul |
| 5 | **5xx oranı** | `http.request` | `level = "ERROR"` (yani `status >= 500`) | 5 dk'lık pencerede **> 5** olay **veya** toplam isteğin **%1**'i | Sunucu bozuk. Panik `middleware.Recoverer` tarafından 500'e çevrilir ve **bu olayda görünür**. 🔴 **Sağlık sondaları bu olayın DIŞINDADIR** — aşağıdaki bloğa bak; `/readyz`'in 503'ü tasarımdır ve bu kuralı ateşlemez, ama `/readyz`'den gelen bir **500** ateşler. | `route` + `request_id`; aynı `request_id` ile o isteğin diğer satırları |
| 6 | **hazırlık kaybı** | `readiness.lost` | olayın kendisi | **≥ 1** olay = uyarı. Pencere yok. Kapanışı `readiness.regained`'dir. | Veritabanı cevap vermiyor, `/readyz` 503 dönüyor ve pod rotasyondan düşüyor (M8-01). 🔴 **M8-03 4. TUR: kayıt artık sürücünün METNİNİ taşımıyor.** Bir tur boyunca taşıyordu ve bu, `health.go`'nun aynı metni kimliksiz bir HTTP çağıranından esirgeme gerekçesiyle çelişiyordu (rol · veritabanı · host · port · TLS duruşu — ölçüldü; **parola yok**). Yerine iki alan geçti: `err_class` **kapalı kümedir** (`server` · `timeout` · `canceled` · `dns` · `dial` · `other`) ve `err_cause` adres taşımayan en özgül sebeptir — `server` için **SQLSTATE** (yanlış parola `28P01`, olmayan veritabanı `3D000`), `dial` için çağrının kendi kelimesi (`connect: connection refused`), `dns` için `no such host`. **Kaybedilen:** sürücünün cümlesi; `other` sınıfı sebep taşımaz. | `err_class`, sonra `err_cause`; ardından bölüm 1'deki `connection refused` ve bölüm 5'teki DNS akışı |
| 7 | **operatör yüzeyi ulaşılamaz** (M10 OP-8) | açılış satırı — `msg` bir cümledir, filtre ALANDADIR | `operator_surface = "unavailable"` | **≥ 1** olay = uyarı. Pencere yok. Kapanışı bir sonraki açılışın `"operator_surface":"configured"` satırıdır. | Operatör yapılandırması tam ama süreç açılırken operatör veritabanına **ulaşılamadı** (ağ, ad çözümü, zaman aşımı, `28P01` yanlış parola, `3D000` veritabanı yok, sunucu dolu/açılıyor/kapanıyor — `internal/db` `unreachableSQLSTATEs`); `/operator` ve altı 503 (`TestSurface_OffAnswers503UnderThePrefixAndNowhereElse`; bu 503 tasarımdır ve 5. kuralı ateşlemez — sınır 32), müşteri ürünü servis verir. **Süreç yeniden denemez:** nedeni düzelt, sonra rollout. Satır süreç açılışında **bir kez** yazılır; log döndürülmüşse sinyal gitmiştir (6. kuralın aynı daralması). | `err` alanı (bir kod: sunucunun mesajını taşımadığını `internal/db`'nin `TestOperatorConnectErr_OnlyTheTableIsUnreachable`'ı tablosunun SQLSTATE'lerinde ölçer; `TestOpenOperatorSurface_PrintsNoValue`'nun sürdüğü değerlerden DSN ve parola ham biçimiyle, iki anahtar base64, hex ve ondalık liste biçimleriyle satırda bulunmadı), sonra *"Operator surface (M10 OP-7)"* bölümünün 5. adımı |
| 8 | **dokunuş yalnız guardrail'lerle karar verdi** (T122) | `policy.guardrails_alone` | olayın kendisi | 15 dk'lık pencerede **≥ 1** olay = uyarı | `forTenant` işletmenin politika kümesini kuramadı (okuyamadı, yazamadı ya da yazdıktan sonra tamamlayamadı/ayrıştıramadı) ve dokunuşa **yalnız guardrail'lerle** karar verdi. Hiçbir baseline ifadesi eşleşemediği için `ok` olacak dokunuş **çoğunlukla `flag` olarak yazılır** ve müdür kuyruğuna düşer (§4.6). ⚠️ Veritabanına **bütünüyle** ulaşılamıyorsa kaydı yazacak `WithTenant` da düşer: kayıt yazılmaz, kişiye yeniden dokunması söylenir — o durumda 6. kural da ateşler. T121'den (`f2c7771`) sonra bilinen zararsız kaynağı yok — eşzamanlı ilk dokunuşların 23505'i kapandı —, yani tek satır gerçek bir sorundur. ⚠️ Panelin `StoredSet` okuması bu olayı **basmaz**: oradaki geri düşüş bir yetki reddidir ve hiç dokunulmamış bir işletmede beklenen durumdur. ⚠️ İsteğin **kendi** bağlamı bittiyse (istemci gitti ya da `httpx.RequestTimeout` doldu) olay da **basılmaz**: o dokunuş zaten kaydedilmez (kaydı yazacak `WithTenant` aynı ölü bağlamla düşer); yerine sayfalamayan bir WARN cümlesi yazılır (`reason` taşır). İstek canlıyken sürücünün kendi zaman aşımı ise sayfalar. | `tenant_id` (hangi işletme) ve `reason` (hangi yol): `load_failed` / `reload_failed` = politika katmanı okunamadı (önce 6. kural ve `sqlstate`; `42501` bir GRANT kaymasıdır) · `materialise_failed` = eksik baseline yazılamadı (`sqlstate` + `constraint` hangi ifadenin düştüğünü söyler; örnekler: `23505` = bir benzersiz indekste çakışma — T121'in kapattığı sınıf geri gelmiş olabilir —, `23503` = aynı adla ama türetilmemiş bir id'yle duran bir `policies` satırı, `db/queries/policies.sql`'in *foreign-id container* notu) · `incomplete_after_materialise` = yazıldı, geri okundu, hâlâ eksik; SQL hatası yok, çoğu zaman saklı bir baseline belgesini bu sürüm okuyamıyor (ör. bir kod geri alımından sonra) — panelin kural sayfası onu gösterir. `sqlstate` ve `constraint` yalnız bir PostgreSQL hatası varsa yazılır; `Detail` **bilerek yazılmaz** (satır değeri taşır) |

**Yapıştırılabilir SigNoz/ClickHouse tarzı filtreler** (alan adları koddaki
sabitlerdir ve `TestObservability_AlertSignalNames` ikisinin aynı kaldığını
denetler; **filtrelerde geçen her `sys:`/`base:` adının `internal/policy`'de
gerçekten var olduğunu** `TestObservability_EverySidInTheAlertRulesExists`
denetler — aşağıdaki nota bak). Bunlar **elle arama** içindir; **kurulu** kuralların tam
SQL'i `deploy/observability/signoz-alert-rules.json`'dadır ve aynı sabitlere bağlıdır
(yukarıdaki kutu):

```
# 1 — reject oranı
body.msg = "tap.decision" AND body.verdict = "reject"

# 2 — flag kuyruğu
body.msg = "tap.decision" AND body.verdict = "flag"      # group by body.matched_sid

# 3 — guvenlik olayi (tek satir bile uyaridir) — sebebi matched_sid soyler
body.msg = "tap.security_alert"

# 4 — ctr sicramasi
body.msg = "tap.decision" AND body.ctr_gap > 10

# 5 — 5xx (saglik sondalari zaten bu olayin disinda)
body.msg = "http.request" AND body.status >= 500          # group by body.route

# 6 — hazirlik kaybi
body.msg = "readiness.lost"

# 7 — operator yuzeyi ulasilamaz (acilista bir kez; tek satir bile uyaridir)
body.operator_surface = "unavailable"

# 8 — dokunus yalniz guardrail'lerle karar verdi (tek satir bile uyaridir) — yolu reason soyler
body.msg = "policy.guardrails_alone"                     # group by body.reason, body.sqlstate
```

> 🔴 **BU TABLODA BİR ZAMANLAR OLMAYAN BİR SID YAZIYORDU, VE ONU KURALI YAZAN TUR
> YAZDI.** 1. satırın *"İlk bakılacak yer"* sütunu operatöre, sonu `tag-lost` olan
> `sys:` önekli bir `matched_sid` değerini yapıştırmasını söylüyordu. Ölçüldü: o ad
> ağaçta **hiç yok** — tek isabeti bu dosyanın kendi satırıydı. Gerçeği
> `sys:tag-not-active`; migration 00013 dördüncü bir plaket durumu (`unassigned`)
> ekleyince guardrail kara listeden **izin listesine** çevrildi ve adı değişti
> (`internal/policy/guardrails.go`). Yapıştırılan filtre **sıfır satır** dönerdi —
> yani bu bölümün kendi açılış cümlesinde tarif edilen arıza, tam da uyarı
> tablosunun içinde.
>
> **Düzeltmek yetmedi, mekanizma kondu.** `TestObservability_EverySidInTheAlertRulesExists`
> bu bölümde geçen **her** `sys:`/`base:` adını çıkarır ve `internal/policy`'nin
> **string sabitlerinde** (yorumlarında değil) aramak zorundadır; bulamazsa derleme
> kırmızıya döner. ⚠️ **Sayılmış iki sınır:** (i) kapsam **bu bölümdür**, dosyanın
> tamamı değil — sınır **23** bu belgedeki atıfların denetlenmediğini sayarken
> kanıt olarak **kasıtlı bir uydurma politika adı** tutuyor ve onu muaf tutmak,
> çürüyen bir muafiyet listesi başlatırdı; (ii) test sid'in **var olduğunu**
> kanıtlar, **doğru sid olduğunu** değil — 2. kurala başka bir gerçek sid yazılsa
> geçerdi. Yanlış-ama-gerçek bir sid bir gözden geçirme sorunudur; olmayan bir sid
> **sessiz** bir sorundur, ve kapanan ikincisidir.
>
> ⚠️ **VE BU TESTİN KENDİ KAPSAMI BU NOTU DA İÇERİR** — yani bu paragrafa örnek
> olsun diye gerçek olmayan bir sid yazılamaz. Kasıtlıdır: kural ne kadar dar
> olursa, istisnası o kadar az olur.

### 🔴 SAĞLIK SONDALARI `http.request` KAYDININ DIŞINDADIR — ve neden

**Bu kararın üç ayrı ölçümü var, üçü de M8-03'ün 1. turuna karşı alındı (2026-08-19).**
O turda `AccessLog` her yanıtı yazıyordu, sondalar dahil. Yanlıştı:

1. **Canlılık, log hedefine bağlandı.** `Handle`'ı 2 sn uyutan bir `slog.Handler`
   ile `GET /healthz` **2,001 sn**'de 200 döndü. Üretimde `os.Stdout` bir konteyner
   log borusudur; node onu boşaltamazsa yazma **bloklar**, `livenessProbe`
   (`timeoutSeconds: 2`, `failureThreshold: 3`) düşer ve kubelet **sağlıklı bir
   süreci öldürür.** Bu, `TestHealthz_CannotDependOnAnything`'in adını taşıdığı
   arızanın gözlemlenebilirlik kartı eliyle geri getirilmesiydi. Düzeltmeden sonra
   aynı sonda log hedefine **hiç dokunmuyor**: 2 sn takılan bir hedefe karşı 21
   ardışık istekte **0,23–0,82 ms** ölçüldü.
   ⚠️ **Buraya TEK bir sayı DONDURULMUYOR, ve bu ölçülmüş bir sürüklenmenin
   düzeltmesidir:** aynı olgu için bu satır **1,1 ms**, `internal/httpx/requestlog.go`
   **0,6 ms**, bağımsız bir üçüncü ölçüm **0,711 ms** yazıyordu — bir olgu, üç sayı,
   çünkü bir geliştirici makinesindeki duvar saati gecikmesi sabit değildir. Duran
   kanıt bir sayı değil, `TestHealthz_AnswersWhileTheLogTargetIsUnavailable`
   testidir: karşılaştırmayı (cevap süresi < 2 sn takılma) her koşuda yeniden hesaplar.
2. **`/readyz`'in TASARLANMIŞ 503'ü "sunucu bozuk" kuralını ateşliyordu.**
   `status >= 500` → `level=ERROR`, ve 5. kural budur. kubelet `/readyz`'i **5
   sn'de bir** yokluyor → geçici bir DB dalgalanması **5 dakikada 60 ERROR**
   üretiyordu, eşik **5**: alarm ~**25 saniyede** çalıyor ve yanlış sebebi
   söylüyordu.
3. **Hacim.** Günde 17.280 `/readyz` + 8.640 `/healthz` = **25.920 istek**, kayıt
   başına ölçülen ~197 bayt → **~5,1 MB/gün**, ve bu **sıfır kullanıcı
   trafiğiyle**. Yani log'un neredeyse tamamı sonda olurdu.

**Bugünkü davranış:** bir sonda **tasarlandığı gibi** cevap veriyorsa kayıt
**yazılmaz** — `/healthz` → 200, `/readyz` → 200 **ve** `/readyz` → 503. Bunun
dışındaki her durum **normal şekilde** kaydedilir; `/readyz`'den gelen bir **500**
(örneğin handler'ın paniklemesi) `level=ERROR` ile 5. kurala düşer. Tablo
`internal/httpx/requestlog.go` içinde (`probeDesignedStatus`) ve
`TestAccessLog_AHealthyProbeIsNotAnEvent` ile
`TestAccessLog_AnUndesignedProbeStatusIsStillRecorded` iki yarıyı da tutar.

**Aynı kural, durumu tasarım olan bir rota için: `/operator`'ın 503'ü (M10 OP-7, 2c).**
Operatör yüzeyi kapalıyken ya da ulaşılamazken `/operator` ve altı her istekte 503 verir;
bu yanıt **tasarımdır** ve kayıt **yazılmaz** — yoksa birleştirmeden itibaren
`/operator`'a gelen altı kimliksiz istek 5. kuralı çaldırırdı (OP-7'den önce bu yol
404'tü). Sondalardan farkı: tasarım rota kalıbına değil yüzeyin **durumuna** bağlıdır (aynı
kalıplar OP-8'de gerçek handler'ları taşıyacak), bu yüzden tabloya değil, yanıtı yazan
handler'ın bildirimine dayanır (`httpx.AnswerAsDesigned`): bildirilen durumdan farklı bir
cevap (ör. paniğin 500'ü) her rotada olduğu gibi kaydedilir. Durumun kendisi görünür kalır:
`cmd/tappa` onu açılışta **bir kez** yazar — kapalıysa INFO, ulaşılamazsa ERROR
(`"operator_surface"` satırı). `TestSurface_ItsDesigned503IsNotAnAlertEvent`,
`TestAccessLog_ADeclaredDesignedAnswerIsNotAnEvent` ve
`TestAccessLog_ADeclarationIsThisRequestsAndThisStatusOnly` tutar. Bedeli sınır **32**'de
sayılı.

🔴 **VE GERÇEK BİR HAZIRLIK ARIZASI GÖRÜNÜR KALIR — kayıt kaybolmuyor, YERİ
DEĞİŞİYOR (§4.6).** Uç noktanın **kendi sahibi** olan `internal/handler.Health`
durum değişimini zaten yazıyor: `readiness.lost` ve `readiness.regained`,
**geçiş başına bir kayıt**, sürücünün hatasının **sınıflandırılmış** hâliyle
birlikte (`err_class` + `err_cause`; ham metin M8-03 4. turda çıkarıldı — 6.
kuralın satırı sebebini yazıyor). Bu, yerini aldığı
erişim kaydından **iki bakımdan daha iyi ve bir bakımdan daha dardır** — üçü de
yazılı, çünkü tek yönlü bir "daha iyi" bir takası gizler. **Daha iyi:** dakikada
12 satır yerine olay başına 1 satır, ve yanıt gövdesinin bilinçli olarak
söylemediği sebep (başlangıç **ve** bitiş kaydı var). **Daha dar:** sinyal olayın
**başladığı ana** bağlıdır, süresine değil — devam eden bir arıza, başlangıcı
sorgu penceresinin dışında kaldıysa **hiç görünmez**, ve o tek satır rotasyonla
düşerse sinyal tümüyle kaybolur. Tam muhasebe **sınır 28**'de. 6. kural onu okur,
eşik **1**. Aynı yolu iki ağla kapatmamak
bu repoda zaten yazılı bir ilkedir (`scripts/redline-check.sh` → R7c).

`/healthz` için kayıp yok, çünkü kaydedilecek bir şey yok: HTTP'ye cevap
veremeyen bir süreç log da yazamaz. O arızayı kubelet bildirir.

⚠️ **1. VE 4. SİNYAL SESSİZCE `TAPPA_LOG_LEVEL`'A BAĞLIDIR.**
`internal/domain/checkin.decisionLevel` `ok` ve `ignored` kararlarını **INFO**,
yalnız `reject`/`flag`'i WARN yazar. 1. sinyal bir **orandır** ve paydası INFO'da
durur; 4. sinyalin `ctr_gap`'i çoğunlukla `ok` tap'larda taşınır. `warn`'a
çekilirse **payda çöker (oran sonsuza dek %100 görünür)** ve **`ctr_gap` sinyali
tamamen kaybolur** — ikisi de ekranda "sakin ve sağlıklı" diye okunur. `format`
için kapalı küme testi vardı, **seviye için yoktu**; artık
`TestObservability_TheShippedLogLevelAdmitsTheSignals` sevk edilen ConfigMap
değerini okuyup INFO'yu kabul ettiğini doğruluyor. Seviye bir **geri düşüştür**
(yazım hatası sessizce `info` demektir), yani kasıtlı bir `warn`'ı yakalayan tek
şey budur.

**Korelasyon:** her satır `request_id` taşır — **eğer** o satır bir `*Context`
çağrısından geldiyse. M8-03 tap karar zincirini (`internal/handler/tap.go`,
`internal/handler/checkin.go`, `internal/domain/checkin`) ve HTTP sınırını
dönüştürdü; **ağacın geri kalanı dönüştürülmedi** (backlog T51: yalnız
`internal/handler`'da 224 çağrı yeri, paket çapında bir dönüşüm). Bir satırda
`request_id` yoksa bu bir arıza değil, **hiç ulaşılamamış** demektir; o satır
`http.request` kaydına zamanla ve `route` ile bağlanır.

⚠️ Bir logger `WithGroup(...)` ile türetilirse `request_id` o grubun **içine**
düşer ve yukarıdaki üst-seviye filtre onu **bulamaz**. Bugün repoda `WithGroup`
çağıran hiçbir yer yok (ölçüldü) ve
`TestWithRequestID_WithGroupNestsTheID` bunu ilk ekleyene söyler.

🔴 **`request_id` GELEN `X-Request-Id` BAŞLIĞINDAN GELEBİLİR — VE M8-03 4. TURA
KADAR SINIRSIZDI.** chi'nin `middleware.RequestID`'si gelen başlığı **olduğu gibi**
kullanıyordu; bu kart o alanı **her erişim kaydına** bağlayınca kimliksiz bir
yazma kanalı açıldı. Ölçüldü (gerçek dinleyici, üretim router'ı, üretim şeklinde
sarmalanmış `slog.JSONHandler`):

| sonda | önce | sonra |
|---|---|---|
| 900 KB'lik `X-Request-Id` + `GET /nope` | **921 757 bayt** tek kayıt | **183 bayt** |
| aynısı, 30 istek (~0,2 sn) | **27 652 710 bayt** (26,4 MiB) | **5 490 bayt** |

Saklama penceresi **10 MiB × 5 = 50 MiB**, yani ~57 istek pencereyi siliyordu —
`tap.security_alert` ve **veritabanı kopyası olmayan** `readiness.lost` dahil.

Yerine `internal/httpx.RequestID` geçti: gelen değer **kabul edilir ama
sınırlanır** — karakter kümesi `[A-Za-z0-9._-]`, uzunluk tavanı
**`httpx.MaxRequestIDLen` = 64**; uymayan değer **sessizce** kendi ürettiğimizle
değiştirilir (istek reddedilmez — reddetmek log hijyenini bir erişilebilirlik
kaldıracına çevirirdi). ⚠️ **Reddetmek yerine sınırlamak bilinçli:** ölçüldü ki
ingress-nginx erişim kaydının **son alanı** `$req_id`'dir ve gelen değeri
**aynen** yazıyor — yani nginx satırı, Tappa'nın §4.7 gereği **hiç yazmadığı**
istemci adresiyle aynı satırda aynı id'yi taşıyor. Gelen başlığı büsbütün atmak,
bir selin **tek** atfedilebilirlik kanalını yok ederdi.

⚠️ **KALAN, SAYILMIŞ:** filtreden geçen bir id hâlâ **çağıranın seçtiğidir**;
saldırgan kendi yol açtığı kayıtlara istediği (kısa, sade) değeri yazdırabilir ve
bir operatörün filtrelediği id ile çakışabilir. Bu bir **rahatsızlık**, sel değil,
ve nginx birleştirmesini korumanın bedelidir.

🔴 **VE `MaxHeaderBytes` ARTIK AYARLI** (`httpx.MaxHeaderBytes` = **16 KiB**;
Go'nun varsayılanı 1 MiB idi). Tek başına S1'i kapatmaz — 1 MiB'lik bir id yine
1 MiB'lik satır demekti — ama kural yazılmamış **başka** başlık kanallarını da
sınırlar. Ölçüldü: sevk edilen ingress bir başlık satırında ~**8 KB**'ı geçiriyor
(8 000 → 404, 12 000 → HTTP/1.1'de 400, HTTP/2'de framing hatası), yani 16 KiB
onun iki katı ve ürünün kendi en kötü hâlinin (çerez kavanozu; en uzun değer
43 karakterlik token) çok üstünde. ⚠️ **Sınır:** bu tavanı aşan istek `net/http`
tarafından **başlık ayrıştırma sırasında** 431 ile kesilir, yani **hiç erişim
kaydı üretmez** (ölçüldü: 0 bayt). Tavanı zorlayan bir çağıran bu süreçte
**görünmez**; isteğin adresi ingress log'unda kalır.

### §4.7 — log'a asla düşmeyenler, ve bugün onları ne tutuyor

| §4.7 sınıfı | Tip düzeyi (derlenmez / basılamaz) | Mekanik tarama |
|---|---|---|
| oturum token'ı | `session.Token`, `adminauth.Token`, `adminauth.ResetToken` → `LogValue`/`String`/`GoString`/`Format`/`MarshalText` | R7 (`token`) |
| `token_hash` | — (dize/bayt) | R7 (`token`) |
| CMAC | — | R7 (`cmac`) |
| AES anahtarı | `sun.Zero` ile silinir; `internal/sun` hata metinleri yalnız uzunluk basar | R7 (`aes_?key`, `secret`) |
| davet kodu | `invite.Code` → aynı beşli | R7 (`invite_?code`, `code[_a-z]*hash`) |
| **tam GPS koordinatı** | **`geo.Point` ve `tenant.GPS` → aynı beşli** (`Format`/`String`/`GoString`/`LogValue`/`MarshalText`; M8-03'te eklendi, 4. turda **beşe tamamlandı**) | **R7c** (M8-03'te eklendi) |

🔴 **GPS satırı M8-03'ten önce TAMAMEN BOŞTU** — ne tip düzeyi bir koruma, ne bir
tarama kuralı vardı. Ölçüldü: bir log çağrısına `"latitude", 35.8997` eklemek
`scripts/redline-check.sh`'i **exit 0** bırakıyordu.

🔴 **VE İLK HALİ YALNIZ `LogValue` İDİ — TABLONUN "aynı beşli" DEDİĞİ ŞEY DEĞİL.**
4. turda ölçüldü: `%v` · `%+v` · `%#v` · işaretçi üzerinde `%v` · `json.Marshal` ·
**değerle bir struct alanının içinde** · `[]Point` · `map[string]Point` — sekizi de
tam koordinatı basıyordu. İkisi **iki ağı birden** deliyordu (`fmt.Sprintf("%v",
*fix)` ve struct-içinde-değerle), çünkü R7c bir eksen ADI arar ve ikisinde de yok.
`Format` ikisini de kapatır. Kalan tek delik yazılı ve **testle sabitlendi**: başka
bir struct'ın **dışa açık olmayan** alanındaki bir `Point`, `%v`/`%+v` altında hâlâ
sızar — `fmt` orada `Formatter`'a hiç danışmaz. `session.Token`'ın çözümü (alanı
`*string` yapmak) burada yok, çünkü `Lat`/`Lng` aritmetik girdidir.

⚠️ **NE TUTMUYOR, SAYILMIŞ HALDE:** R7/R7b/R7c **metin** taramalarıdır. Üçünün de
eşleşme penceresi **bir seviye** dengeli parantez taşır (R7 4. turda hizalandı);
**iki seviye** iç içe parantezin arkasındaki bir tetikleyici hâlâ görünmez. Ara
değişkene kopyalanmış bir değer üç kuralın üçünde de görünmez. **Gerçek çözüm tip
düzeyi logger redaksiyonudur** (backlog T51, M8-04) ve bu ağlar onun **yerine
geçmez**.

🔴 **R7 GENİŞLETİLDİ (M8-03, 4. TUR) — VE ÖNCEKİ TURDA "YAPILMADI" DİYE
YAZILMASI BİR HATAYDI.** R7 §4.7'nin **en sert beş sınıfını** taşıyor (oturum
token'ı · `token_hash` · CMAC · AES anahtarı · davet kodu) ve R7b/R7c'ye ödenen
bedelin **ikisini de** ödememişti: `-U` yoktu ve pencere ilk `)` karakterinde
duruyordu. Bir güvenlik denetçisi beş sınıfın **beşini de** tek yazımla geçirdi —
tetikleyiciyi `id.EmployeeID()` gibi bir çağrının **arkasına** koymak yetti, ki bu
`internal/domain/checkin/checkin.go`'daki tap karar kaydının **bugünkü** yazımı.
**Ölçek — ve YÖNTEMİYLE, çünkü sayının kendisi bir tur yanlış yazıldı.**
*"Çok satırlı"* burada tek bir şey demek: bir logger çağrısının **açılış** parantezi
ile **kapanış** parantezi farklı kaynak satırlarında. Ölçüm, bu reponun kendi tarama
kapsamıyla aynı AST yürüyüşüdür (`_test.go` · `*_templ.go` · `internal/store/` dışarıda;
alıcı adında *"log"* geçen `Info|Warn|Error|Debug|Log|*Context|LogAttrs` çağrıları —
`cmd/tappa/observability_test.go`'nin kapsamı). **Kart öncesi ağaçta 346 çağrı yerinin
137'si** çok satırlı, yani argümanları R7'ye **tanımı gereği** görünmezdi. (Kart
sonrası ağaçta **349'un 141'i**.)
⚠️ **[GERİ ÇEKİLDİ — bu cümle bir tur boyunca **131** dedi ve yöntemini yazmadı.]**
Yöntem yazılmadığı için sayı *"yanlış"* değil **yeniden üretilemez**di; 5. turda beş
makul tanım denendi ve **hiçbiri 131 vermiyor**: paren aralığı **137** · düğüm aralığı
**137** · yalnız `internal/` **135** · alıcısı `log`/`slog`/`fmt` olanlar **137** ·
ilk argümanı alt satırda olanlar **0**. Yazılan sayı **137**'dir ve yöntemi yukarıda.

Ölçülen bedel (2026-08-19, `SRC`/`GEN_EXCLUDE` aynı):

| R7'ye ne uygulanırsa | Yanlış pozitif | Hangileri |
|---|---|---|
| eski hali (dar pencere, `-U` yok) | **0** | — ama beş sınıfın beşi de kaçıyordu |
| yalnız **`-U`** | **2** | `test/fixtures/seedkeys/main.go`'nun seed `UPDATE`'ini yazan `fmt.Fprintf` bölgesi · `internal/domain/signup/signup.go`'nun `MinPasswordRunes`'u biçimleyen `errs.add("password", …)` bölgesi |
| **`-U` + dengeli parantez penceresi** (sevk edilen) | **3** | yukarıdakilere ek: `internal/adminauth/password.go`'nun `ErrPasswordTooLong`'u saran `fmt.Errorf` bölgesi |

⚠️ **Bu tablo satır numarası TAŞIMIYOR, ve bu bilinçli.** Önceki hâli
`seedkeys/main.go:132` diyordu; o satır bugün bir **yorum**, gerçek bölge
`fmt.Fprintf` çağrısında. Bu dosyanın `deploy.yml` dersi aynen geçerli: **satır
numarası yerine sembol/adım adı**. Muafiyetlerin bugünkü konumu her koşuda
`scripts/redline-check.sh`'in kendi WARN çıktısında basılıyor — canlı kaynak orası,
bu tablo değil.

Üçü de **muaf edildi ve BİREBİR ADLA yazıldı** (`R7_WAIVERS`), toplu bir
`--glob !` ile değil, ve **her koşuda WARN olarak basılır** — R1'in 2026-08-13'te
öğrendiği kural. Muafiyet *"bu satırı görmezden gel"* değil: bir bölge ancak
(1) yolu listelenmişse, (2) o girdinin **adı geçen** masum belirteçleri
çıkarıldığında (3) geriye **hiçbir** tetikleyici kalmıyorsa muaftır — yani muaf
dosyaya gerçek bir `token`/`cmac` eklenirse bölge yine FAIL üretir.

| muafiyet | belirteç | neden yanlış pozitif |
|---|---|---|
| `test/fixtures/seedkeys/main.go` | `aes_key_ref` | bir **sütun adı**. Bölge `fmt.Fprintf(&b, ...)` ile bir string buffer'a **seed SQL'i** yazar; basılan değer `hex.EncodeToString(ref)`, yani KEK ile sarmalanmış **referans**, ham anahtar değil. Hedef log değil. |
| `internal/adminauth/password.go` | `ErrPasswordTooLong` | bir **sentinel hata değişkeni**. `fmt.Errorf("%w (got %d bytes)", ErrPasswordTooLong, n)` — biçime giren tek şey sentinel ve bir **uzunluk**; parola `n`'e kasıtla hoisted edilmiş. |
| `internal/domain/signup/signup.go` | `"password"`, `MinPasswordRunes` | biri **form alanı anahtarı** (hata haritasının anahtarı), diğeri bir **sabit int**. Eşleşen çağrı `fmt.Sprintf("Use at least %d characters...", MinPasswordRunes)`; `a.Password` bu çağrıya hiç girmiyor. |

⚠️ **INVARYANT TESTİ `_test.go` DOSYALARINI GÖRMÜYOR, R7/R7b/R7c GÖRÜYOR — SAYILMIŞ
SINIR.** `TestObservability_EveryLoggerCallSiteIsSpelledLog` yürüyüşü `_test.go`
ile biten her dosyayı atlar; `scripts/redline-check.sh`'in `SRC` listesi ise
`_test.go`'ları **kapsar** (`GEN_EXCLUDE` yalnız `*_templ.go` ve
`internal/store/*.go` çıkarır). Sonuç: bir test dosyasına yazılan
`a.logger.Error(..., r.PostFormValue(...))` **hem** üç metin kuralının **hem de**
invaryantın kör noktasındadır. Kapatılmadı, çünkü kapatmak testlerdeki alıcı
adlandırmasını bir kurala bağlamak demek ve bu kartın işi değil; **sınır olarak
yazıldı**. ⚠️ Bir test dosyasındaki bir sızıntı üretime çıkmaz, ama gerçek bir
kişisel veriyle koşan bir DB testi aynı süreç log'una yazar.

---

## Kabul edilmiş sınırlar — hepsi bilinçli, hiçbiri kaza

1. **[yedek kümede HENÜZ YOK]** — 🔴 **BU MADDE ESKİ *[yedek YOK]* MADDESİNİN
   YERİNDE DURUYOR, VE ANAHTARIN DEĞİŞME SEBEBİ ANAHTARIN ARTIK YALAN OLMASI.**
   Eski madde *"Yedek YOK, replika YOK"* diyordu ve **bugün ağaç için yanlış, küme
   için hâlâ doğru** — bu dosyada üç kez bedeli ödenmiş ayrım (bkz. sınır
   **[Harbor k8s-2'ye bağlı]**, eski adı *[Docker Hub anonim çekme bütçesi]*), o yüzden ikisi ayrı
   yazılıyor.
   ✅ **AĞAÇTA olan (M8-02 FAZ E):** `k8s/50-backup.yaml` gecelik bir CronJob,
   `../scripts/pg-backup.sh` dump'ı alıp **içini doğruluyor** (tablo kümesi, tablo
   tablo satır sayısı, politika/`FORCE`/GRANT sayıları, tamamlanma damgası),
   `../scripts/pg-backup-ship.sh` onu **node dışına** taşıyıp saklama süresini
   uyguluyor, ve `../scripts/pg-restore-verify.sh` geri yüklenmiş bir veritabanının
   kaynakla **birebir** olduğunu ölçüyor. Geri yükleme **fiilen denendi** — üç
   kontrollü koşu, biri kasıtlı negatif kontrol; sayıları *"Yedek ve geri yükleme"*
   bölümünün son tablosunda.
   ❌ **KÜMEDE olmayan, ve ölçümüyle:** `kubectl -n tappa get cronjob` →
   **`No resources found`** (2026-08-16, salt okuma). Operatör adımı 9 iki şey
   istiyor ve ikisi de ajanın yapamayacağı şeyler: **off-node bir hedef + kimliği**
   (`secret/tappa-backup-target`) ve `kubectl apply -f deploy/k8s/50-backup.yaml`.
   🔴 **M8-06 pilotu başlamadan kapatılmalı** — pilot bir haftalık paralel kayıt
   üretiyor ve o hafta §4.3 gereği yeniden kurulamaz.
   ⚠️ **Bu maddenin KAPANMAYAN yarısı ayrı:** `local-path`, **tek node**,
   `reclaimPolicy: Delete`, replika yok, yani §1'in *"managed Postgres"*i hâlâ
   karşılanmıyor (kullanıcı kararı, uyarıldıktan sonra). Bir gecelik yedek, node
   ölürse **son 24 saate kadar** kayıp demektir; senkron replika demek değildir.
   Ayrıntı `k8s/10-postgres.yaml` başında.
2. **`TAPPA_RETENTION_YEARS=2` GEÇİCİ.** Bu sayı çalışana GDPR Art. 13 metninde
   gösteriliyor, yani hukuki bir beyan; hukukçu onayı bekliyor (Q13 / backlog B3).
   Deploy için kabul, **pilot için değil** — M8-06 kapısının maddelerinden biri.
3. **~~`TAPPA_RESET_DELIVERY=none` (ve `TAPPA_INVITE_DELIVERY=panel`)~~ — KAPANDI,
   2026-10-09:** iki akış da `email` (kullanıcı kararı, EM-5B; ADR 0022 *"EM-5B notu"*).
   Q02'nin cevabı ADR 0022 (AWS SES, `eu-central-1`, SMTP + STARTTLS); sıfırlama kanalı
   EM-5, davet kanalı EM-7B, süreç geneli kesici EM-7A, alıcı başına tavan EM-7C, gerçek
   SES ölçümü EM-5B. **Kalan:** canlı duman kontrol listesi ve o bölümün sayılı sınırları
   — runbook *"Transactional e-mail (M10 EM-3)"* → *"3) Akışı aç"*. (Madde numarası,
   atıflar çözülsün diye yerinde duruyor.)
4. **HSTS ingress'ten miras alınıyor** (`max-age=31536000; includeSubDomains`,
   ölçüldü). Uygulamanın **kendi** başlığını set etmesi (backlog T28) hâlâ açık;
   `preload` set edilmedi ve kolayca set edilmemeli.
5. **Tek replika.** `internal/domain/legal` yayımlanmış yasal metinleri süreç içi
   bir anlık görüntüden sunuyor ve *"ikinci bir süreç eski metni yeniden başlayana
   kadar sunar"* diyor. İki replika iki farklı gizlilik politikası demek olurdu.
   Gerekçe ve maliyeti `k8s/20-app.yaml` başında.
6. **[`make audit` yerelde kırmızı]** — **CI ile imaj artık AYNI Go'da; kırmızı
   olan yalnız GELİŞTİRİCİ MAKİNESİ.** ⚠️ Burada eskiden *"CI'nin Go'su 1.26.5"*
   yazıyordu ve bu **yanlıştı**: `.github/workflows/ci.yml:91` → `go-version:
   "1.26.6"` (commit `6087d07`), yani CI ile Dockerfile aynı sürümü pinliyor ve
   **sevk edilen ikili** o altı stdlib açığını taşımıyor. Geriye kalan tek şey,
   `make audit`'in geliştiricinin **yerel** Go kurulumunu sayması (backlog T31).
   Yanlış sayılmış bir açık, kapanmadığı sanılan bir açıktır — bu satır düzeltildi
   diye T31 kapanmaz, ama artık doğru şeyi tarif ediyor.
7. **`sslmode=disable`** — bağlantı bu node'un kendi köprüsünden çıkmıyor
   (tek node, pod↔pod `cni0`). Yönetilen bir Postgres'e taşınırsa `verify-full`
   gerekir; CA paketi imajda **zaten var**.
8. **[kubeconfig'in kimliği doğrulanmadı]** — 🔴 **Daraltmanın YARISI yapıldı ve
   hangi yarı olduğu burada yazılıyor.** ✅ **Yapılan:** deploy'un ihtiyaç duyduğu
   kimlik artık bir **dosya** — `k8s/01-rbac.yaml` — ve her yetkisi ölçülerek
   daraltıldı (`secrets` tam CRUD → tek isim üzerinde `get`; `pods` yedi fiil → tek
   `list`; `pods/exec` tek pod adına kilitli; `serviceaccounts`, `events`,
   `replicasets`, `persistentvolumeclaims`, `externalsecrets` ve `*/status`
   **tamamen** çıktı). ❌ **Yapılmayan ve BU DOSYADAN DOĞRULANAMAYAN:** GitHub
   secret'ı `KUBE_CONFIG`'in **içinde hangi kimliğin durduğu**. İçindeki kubeconfig
   hâlâ cluster-admin ise `01-rbac.yaml` **hiçbir şeyi sınırlamaz** — cluster-admin
   bir kimlik Role'ü aşar ve RBAC ona bakmaz bile. Bu ajanın secret'ı okuma yolu
   yok, yani *"daraltıldı"* demek ölçülmemiş bir iddia olurdu. Kapatan adım
   operatörün: token'ı `kubectl -n tappa create token github-deployer` ile üret,
   kubeconfig'i ondan kur, secret'ı öyle yaz (*"Operatörün yapması gerekenler"*
   **5. adım**). ⚠️ Buradaki atıf **adım numarasıyla** verilmiş olsa da hedefin
   **adı** da yazılı (`KUBE_CONFIG`), çünkü bu listede numara kaymaları daha önce
   beş çapraz atfı yanlış maddeye düşürmüştü.
9. **[KEK döndürme KÜMEYE karşı hiç koşulmadı]** — 🔴 **ARAÇ VAR; AÇIK KALAN
   ŞEY İKİ SAYILMIŞ KALEM.**
   ⚠️ **AĞAÇ/KÜME AYRIMI (sınır 1 bunu yapıyor, bu madde YAPMIYORDU):** aşağıdaki
   *"kapandı"* kalemlerinin hepsi **bu depodaki ağaç** hakkındadır. **Küme bugün bu
   değişikliğin ÖNCESİNDE**: canlı Deployment **dört** env adı enjekte ediyor ve
   çalışan ikili `kek_rotation_window=` satırını **yazmıyor** (ölçüldü, ön koşul
   5c). Yani manifest + imaj sevk edilene kadar kapı `0/0` okur ve **durdurur**.
   ✅ **Kapandı (ağaçta):** `cmd/rotatekek` · **`scripts/rotate-kek.sh`** (prosedür
   artık yapıştırılan bir metin değil, belirtilmiş bir kabukta koşan bir dosya) ·
   iki-KEK okuma yolu (`TAPPA_TAG_KEK_PREVIOUS`,
   `sun.UnwrapAny`) · `20-app.yaml` o adı `optional: true` ile **enjekte ediyor**
   ve `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` sınıfı
   kapatıyor (`internal/config`'in okuduğu her ad bir manifestte olmak zorunda) ·
   üretilen SQL, oturumun **gerçekten RLS'i atladığını** kendi içinde zorunlu
   kılıyor · açık pencere kendini **her 15 dakikada** log'a yazıyor ·
   prosedür *"KEK döndürme"* bölümünde, ön koşul/komut/**adım 3'ten ÖNCE**
   doğrulama/geri alma/durma kuralıyla.
   ❌ **KAPANMAYAN 1 — prosedür bu kümeye karşı hiç koşulmadı.** Yerel bir
   Postgres konteynerine karşı koşuldu. Ölçülen her şey **mekanizma**
   hakkındadır; **küme hakkında tek iddia yoktur.** Kapatan tek ölçüm:
   operatörün `tappa-postgres:5432`'ye **nereden** ulaştığı —
   `12-networkpolicy.yaml` 5432'yi yalnız `tappa` namespace'inde etiketli
   pod'lara açıyor, `kubectl port-forward`'un bu kuralı aşıp aşmadığı **hiç
   ölçülmedi**, ve **`cmd/rotatekek` hiçbir imajda yok** (`Dockerfile` yalnız
   `bin/tappa`), yani araç bir checkout + Go olan bir makinede koşar.
   ❌ **KAPANMAYAN 2 — rotasyon YEDEKLERİ döndürmez.** Rotasyon öncesi her dump
   `aes_key_ref`'i **eski KEK altında** taşır ve içindeki plaket anahtarı aynı
   anahtardır, yani *"sızmış KEK + saklanan herhangi bir eski dump = parkın
   tamamının düz NTAG anahtarı"*, rotasyondan **sonra** da. Runbook'un **adım
   4'ü** iki seçeneği (imha / `BACKUP_RETENTION_DAYS` boyunca sızmış KEK'i canlı
   say) adıyla yazıyor, ama bu kümede yedek CronJob'ı **yok** (**sınır 1**, ölçüm:
   `kubectl -n tappa get cronjob` → `No resources found`; burada da eskiden
   yanlışlıkla **sınır 13** yazıyordu), yani adım **hiç koşulmadı**.
10. **[`:8080` küme içinden açık]** — **Uygulamanın `:8080`'i küme içinden
    erişilebilir.** NetworkPolicy yalnız Postgres'e yazıldı. Şiddeti düşük (aynı
    içerik zaten internete açık, prod'da çerez `Secure`, panel düz HTTP girişini T38
    gereği zaten reddediyor); atlanan şey TLS ve HSTS.
    ✅ **Bu maddenin eski gerekçesi ÖLÇÜLEREK ÇÜRÜTÜLDÜ.** Burada
    *"NetworkPolicy'nin bu kümede uygulandığı `apply` etmeden doğrulanamadı"*
    yazıyordu; **2026-08-16'da doğrulandı ve kural UYGULANIYOR** (tek pod, tek an,
    izin verilen etiketle: kendi namespace'ine `accepting connections` exit 0, canlı
    `tappa` namespace'ine `no response` exit 2 — adı önce `10.42.0.228`'e çözülerek,
    yani DNS değil kural reddediyor). **Yani engel artık "ölçemiyoruz" değil.**
    🔴 **Ölçülmemiş olarak duran şey açıkça yazılıyor (bunun *tek* boşluk olduğu
    iddia edilmiyor — yalnız bilinen ve kapatılmamış olan):** sunucunun `httpGet`
    probe'ları kubelet tarafından **node'dan** yapılıyor ve dar bir ingress kuralının
    o probe'ları da düşürüp düşürmeyeceği **BU TURDA ÖLÇÜLMEDİ**. Önceki tur
    ingress-nginx'in kaynak adresini ölçmüştü (`10.42.0.1`, cni0 köprüsü) ama
    **kubelet'in probe kaynağı ayrı bir sorudur ve ölçülmemiştir** — "muhtemelen
    aynıdır" demek bu listenin varlık sebebine aykırı olurdu. Kapatmak isteyen önce
    onu ölçsün.
11. **[owner DSN'i elle yazılabilir]** — **`DATABASE_URL`'e elle owner DSN'i
    yazılırsa ürün itiraz etmez.** `config.Load`
    yalnız **eşitliği** reddediyor. Kapatan şey bir **deploy kapısı**
    (`deploy.yml` → *"the running server must not be connected as the migration
    role"*, `pg_stat_activity` üzerinden), yani **deploy anına** özgü; deploy sonrası
    değiştirilen bir değeri yakalamaz. Süreç içi kontrol bilinçli olarak
    **yapılmadı** — gerekçesi kartta, ölçümüyle.
12. **[Harbor k8s-2'ye bağlı]** — OPS-2 (2026-10-19). 🔴 **BU MADDE İKİ ESKİ MADDENİN
    YERİNDE DURUYOR:** önce *[GHCR çekme kimliği ömürlü]* (T43: `secret/ghcr`'ı her deploy kendi
    `GITHUB_TOKEN`'ıyla yazıyordu, token iş bitince ölüyordu; kubelet KEP-2535
    `NeverVerifyPreloadedImages` ile koştuğu için her yeniden başlatma ve `rollout undo` bir `401`
    riskiydi), sonra *[Docker Hub anonim çekme bütçesi]* (çare public Docker Hub'dı; bedeli IP başına
    saatte 100 anonim çekme, `x-ratelimit-limit: 100;w=3600`). İmajlar artık **private** bir Harbor
    projesinde (`registry.everva.com.tr/tappa`), pod'lar operatörün `harbor-pull`'uyla çeker
    (operatör adımı 4).
    ✅ **Kapanan:** ürün imajları Docker Hub'ın anonim bütçesinden **harcamıyor**
    (`postgres:17-alpine`, `rclone`, `curlimages/curl` hâlâ oradan — madde 14); `:sha-*` etiket
    listesi herkese açık değil (madde 18); T43'ün kökü — kimliği **her deploy'da yeniden yazan**
    adım — yok ve dönmüyor: CI hiçbir Secret yazmaz (`01-rbac.yaml`: sırlarda yalnız `get`).
    İmajın private olduğu ve CI kimliğiyle çekilebildiği **her koşuda iki yönlü ölçülüyor**
    (`verify-image.sh --access`; operatör adımı 4 (e)); `harbor-pull`'un varlığı kümeye bir şey
    uygulanmadan ölçülüyor (`verify-deployment.sh pull-secret`).
    ❌ **Kapanmayan, sayılı:**
    (a) **k8s-2 bir bağımlılık.** Harbor k8s-2'de koşar. k8s-2 kapalıyken deploy **derleyemez**
    (taban imaj Harbor'ın proxy projesinden gelir — (e)) ve **yeni** bir imaj çekilemez: deploy'un
    migration Job'ı `ImagePullBackOff`'a düşer, *"Migrate"* adımı `ImagePullBackOff`'u gördüğü ilk ankette kırmızı biter
    (OPS-2 düzeltme turu 1; önceden 600 sn bekliyordu), sunucu rollout'u **yapılmaz** (çalışan pod'a
    dokunulmaz). Çalışan pod'un yeniden başlaması
    düğümün **önbelleğindeki** kopyayla açılır — `IfNotPresent` ve değişmemiş `harbor-pull` ile
    Harbor'a gidilmez (KEP-2535: kayıtlı kimlik aynı). **Ölçülmedi:** bu kümede k8s-2 kapalıyken bir
    pod yeniden başlatılmadı; *"OPS-2 kapanış ölçüsü"* (5) onu ölçmeyi önerir. Düğüm imajı
    kaybederse (kubelet imaj GC'si, `crictl rmi`, düğüm yeniden kurulumu) k8s-2 kapalıyken pod
    **açılamaz**.
    (b) **Robot döndürme bir doğrulama çekmesi doğurur.** `harbor-pull`'un içeriği değişince, eski
    değerle çekilmiş bir imaja dönen pod Harbor'a sorar (KEP-2535). Operatör adımı 4 (d) eski robotu
    yeni Secret dağıtılana dek yaşatarak döndürür. İki robot da **süresizdir** (Expiration: Never,
    OPS-2 düzeltme turu 1), yani pencere bir takvimle kendiliğinden açılmaz — yalnız bir Refresh, bir
    devre dışı bırakma ya da silme açar; bedeli, sızan bir değerin de kendiliğinden ölmemesidir.
    (c) **Saklama: depo başına son 30 push.** Daha eski bir `sha-*`'e Harbor'dan dönülemez;
    `rollout undo` düğümde hâlâ duran bir imaja döner (Harbor'a gitmez), başka bir düğümde ya da
    imaj GC'den sonra döndüremez.
    (d) **OPS-2 öncesine geri dönüş Docker Hub'a bağlı.** OPS-2'den önceki ReplicaSet'ler
    `docker.io/atknatk/...` + `imagePullSecrets`'sız; `rollout undo` onlara anonim döner — ta ki
    Docker Hub depoları silinene ya da private yapılana dek (*"OPS-2 geri dönüş"*).
    (e) **Derlemenin taban imajı — ✅ KAPANDI (OPS-2 düzeltme turu 1, kullanıcı kararı: Harbor proxy
    cache).** OPS-2'nin ilk turunda giriş yalnız Harbor'a olduğu için `golang:1.26.9-bookworm` runner'ın
    paylaşılan adresinden **anonim** çekiliyordu (`ci.yml` aynı adreslerden `toomanyrequests`
    ölçmüştü, 2026-10-09). Şimdi *"Build both images"* onu Harbor'ın private proxy projesinden
    (`registry.everva.com.tr/dockerhub/library/golang:1.26.10-bookworm@sha256:5b1f78db…b505` —
    2026-10-21'den beri, GO-2026-6809; önce 1.26.9 `d9c68c2c…453c`), CI
    robotunun girişiyle ve **index digest'iyle** çeker; `Dockerfile`'ın varsayılanı aynı digest,
    Docker Hub'ın adıyla (operatör adımı 4). Önbellekteki bir digest'i Harbor Docker Hub'a gitmeden
    vermeli (proxy cache'in belgelenen davranışı; **ölçülmedi**). İlk OPS-2 deploy'u (2026-10-19)
    yalnız şunu ölçtü: `library/golang`'da görünen (tek, etiketsiz) artifact, sabitlenmiş index'in
    **linux/amd64 çocuk manifest'idir** (kapanış ölçüsü madde 2); index'in hiç saklanmadığı
    **ölçülmedi**.
    **Kalan, sayılı:** (1) derleme artık k8s-2'ye de bağlı ((a));
    (2) ✅ **kapandı (OPS-2 takibi, 2026-10-19):** Dockerfile'ın eski ilk satırı (`syntax` ayrıştırıcı
    yönergesi, `docker/dockerfile:1`) BuildKit'in ön ucunu (frontend imajı) Docker Hub'dan kimliksiz ve
    hareketli etiketle çekiyordu; satır **kaldırıldı** ve derleme BuildKit'in **yerleşik** ön ucuyla
    koşar — hiçbir ön uç imajı çekilmez, ne Docker Hub'dan ne Harbor'dan. Ölçüldü (docker 29.8.2,
    BuildKit v0.33.1): yönerge erişilemez bir registry'ye çevrilince derleme talimatları okumadan
    *"failed to resolve source metadata"* ile düşer; yönergesiz dosyayı yerleşik ön uç ayrıştırır
    (`--call=targets` beş aşamayı listeler, `--call=check` uyarı vermez). Dosya yerleşik ön ucun
    fazlasını istemez (çok aşamalı `FROM`, `FROM`'dan önce `ARG`, `COPY --from`). Proxy'ye digest'le
    pinlemek yerel derlemede Harbor kimliği isterdi, ön ucu dışarıdan seçen bir build-arg ise aynı
    çekmenin ikinci bir yazımı olurdu — ikisi de reddedildi. Pin: `cmd/tappa`
    `TestPackaging_TheDockerfileNamesNoFrontendImage`;
    (3) `postgres:17-alpine`, `rclone`, `curlimages/curl` kümede Docker Hub'dan anonim (madde 14).
13. **[her yeni iş yükü kendi beklemesini taşır]** — 🔴 **Bu namespace'te Postgres'e
    bağlanan HER YENİ İŞ YÜKÜ kendi bekleme adımını taşımak zorunda.**
    `12-networkpolicy.yaml` uygulanıyor, ama k3s yeni bir pod'un adresini kuralın izin
    kümesine **eşzamansız** yazıyor: ölçüldü, izin verilen etiketi taşıyan beş taze
    pod'un **beşi de** ilk denemesinde reddedildi, 0,2–1,0 sn sonra kabul edildi
    (kural silinmiş kontrol: üç pod, üçü de sıfırıncı denemede bağlandı). İlk işi
    veritabanına bağlanmak olan bir süreç bu yarışı **kaybeder**;
    `restartPolicy: Never` taşıyan bir Job ise **her zaman** kaybeder, çünkü her
    yeniden deneme yeni adresli yeni bir pod'dur. Bugün migration Job'ı ve sunucu
    Deployment'ı `wait-for-postgres` initContainer'ı taşıyor. ✅ **Ve bu madde
    tahmin ettiği şeyi fiilen yakaladı:** sınır **[yedek kümede HENÜZ YOK]**'un
    kapatılması için eklenen `k8s/50-backup.yaml` CronJob'ı **aynı initContainer'ı
    taşıyor**, `restartPolicy: Never` ve `backoffLimit: 1` ile — yani onsuz *"sessizce
    ve tekrarlanabilir biçimde düşen"* tam olarak bir gecelik yedek olurdu. Belirti
    `connection refused`'dır, zaman aşımı değil — bkz. *"Olay müdahalesi"*.
    ⚠️ **Kural gelecekteki her iş yükü için hâlâ açık**, kapanmadı: bu, üçüncü
    tüketicinin kurala uyduğunun kaydıdır, kuralın gereksizleştiğinin değil.
14. **[`postgres:17-alpine` hareketli etiket]** — 🔴 **Bu tur, reponun kendi adını
    koyduğu kusur sınıfından bir örnek EKLEDİ ve kapatmak yerine SAYIYOR.**
    `wait-for-postgres` initContainer'ları `postgres:17-alpine`'ı **digest'siz**
    referans ediyor ve `imagePullPolicy: IfNotPresent` taşıyor — yani
    `20-app.yaml`'ın ürün imajı için *"bir düğümün sessizce geçen haftanın ikilisini
    servis etmesi"* diye **reddettiği** bileşimin ta kendisi, üstelik aynı pod'da.
    **Neden tolere ediliyor — taşıyıcı gerekçe ÖNCE:**
    **(a) Blast radius bir sağlık sondası.** Bu konteyner yalnız `pg_isready`
    koşuyor; eski bir kopya da bir adı çözüp bir soket açar. Kusur sınıfı *"bayat
    ikili KULLANICIYA servis eder"* olduğunda ısırır ve burada kimseye hiçbir şey
    servis edilmiyor. **Tek başına taşıyıcı olan gerekçe budur.**
    **(b) Bu namespace'e yeni bir bağımlılık getirmiyor:** `k8s/10-postgres.yaml`
    aynı etiketi zaten koşuyor, yani veritabanı ayaktayken imaj **daima** düğümde —
    ve denetim doğruladı ki kubelet imaj GC'si **kullanımdaki** bir imajı geri
    alamaz. Çekilemediği bir durumda zaten beklenecek veritabanı da yoktur.
    **(c) Digest'e pinlemek AÇIK BİR SERTLEŞTİRME OLARAK DURUYOR**, gözden kaçmadı:
    index digest'i ölçüldü (`sha256:18cfe3ef5e6815…`).
    ⚠️ **Ve buradaki eski gerekçem yanlış yeri gösteriyordu**, düzeltiliyor: *"yalnız
    iki initContainer'ı pinlemek iki yazım bırakır"* yalnız **o varyant** için
    doğruydu — ölçüm şu ki `grep 'image:'` **üçünü de** (`10-postgres.yaml`,
    `20-app.yaml`, `30-migrate-job.yaml`) ve `docker-compose.yml`'i
    `postgres:17-alpine`'da buluyor, yani **üçünü birden** pinlemek dizinde **tek
    yazım** bırakırdı. Ve *"digest'li çekme de oran bütçesinden harcar"* doğru ama
    **konu dışı**: digest pinleme oran limiti için değil, **hangi byte'ların
    koştuğu** için yapılır — reponun kendi tanımladığı kusur sınıfı bir **içerik
    kimliği** sorunudur. Yani kararı taşıyan (a)'dır; (c) kapatılmamış bir
    sertleştirmedir, çürütülmüş bir seçenek değil.
    **Ayrıca sayılan ikinci kanal:** Docker Hub anonim bütçesi, bu ağdan ölçüldü →
    `x-ratelimit-limit: 100;w=3600`. Düğüm imajı kaybederse bu bütçe pod'un
    açılmasıyla arasına girebilir.
    ⚠️ **VE BU TUR SINIFA İKİNCİ BİR ÖRNEK EKLEDİ, SAYARAK:** `k8s/50-backup.yaml`'ın
    `ship` konteyneri **`rclone/rclone:1.71`** — digest'siz, hareketli bir minor
    etiketi, ve `IfNotPresent`. Taşıyıcı gerekçe (a) ile **aynı**: bu konteyner bir
    dosyayı kopyalar, kimseye bir ikili servis etmez. **Ama bu üçüncü yazım**, yani
    dizindeki `postgres:17-alpine` yazımlarını digest'e pinlemek artık **tek yazım**
    bırakmıyor — pinlemeyi düşünen, dördünü birden düşünmeli.
15. **[migration arızası artık ~10× geç bildiriliyor]** — `wait-for-postgres` her
    denemede **120 sn**'ye kadar bekliyor ve Job `backoffLimit: 2` (üç deneme,
    aralarında 10 + 20 sn backoff), yani **gerçekten ulaşılamaz** bir veritabanında
    deploy'un *"migration Job failed"* diyebilmesi ~**390 sn** sürüyor. Ölçülen
    emsal, bu bekleme eklenmeden önce, canlı Job'dan okundu
    (`.status.startTime` → `.status.conditions[Failed].lastTransitionTime`):
    `08:38:54Z → 08:39:32Z` = **38 sn**.
    `activeDeadlineSeconds: 600` ve iş akışının 600 sn'lik anketi hâlâ yeterli, ama
    pay **~565 sn'den ~210 sn'ye** düştü. Bedel: gerçek bir kesintide teşhis altı
    dakika geç başlıyor. Kazanç: bölüm 1 ve 5'teki geçici pencerelerin **hiçbiri**
    artık deploy'u düşürmüyor. Bilinçli takas, burada sayılıyor.
16. **[deploy kimliği namespace'in sırlarını okuyabilir]** — 🔴 **KAPATILAMADI,
    SAYILIYOR (§4.7).** `k8s/01-rbac.yaml` deploy kimliğinin `secrets` yetkisini tam
    CRUD'dan **tek isim üzerinde tek fiile** indirdi (`get` / `tappa-secrets`), yani
    `tappa-secrets` artık bu kimlik tarafından **yaratılamaz, ezilemez, silinemez** —
    ve yeniden üretilemeyen tek değer (`TAPPA_TAG_KEK`; kaybı parktaki her plaketin
    fiziksel olarak yeniden encode edilmesi demek) bu yolla yok edilemez.
    **Ama OKUMA kapanmadı ve buradaki hiçbir RBAC düzeni kapatamaz.** Gerekçe
    yapısal: deploy'un merkezî eylemi bir `Job` yaratmaktır (migration'ın kendisi),
    bir Job'ın pod şablonu **kendi namespace'indeki her Secret'a** referans verebilir
    — bu referansın RBAC'i yoktur, kubelet çözer — ve deploy başarısız bir
    migration'ı raporlamak için o Job'ın pod **logunu okumak zorundadır**. Kum
    havuzunda uçtan uca ölçüldü: bu kurallarla (`jobs: create` + `pods/log: get`)
    yaratılan bir Job, aynı kimliğin **listeleyemediği** bir Secret'ın değerini
    `secretKeyRef` ile alıp loguna bastı. **İki cümlelik sonucu:** `secrets: get`
    (tek isim) ve `pods/exec: create` (tek pod adı) **yeni bir erişim açmıyor**;
    daraltmanın kazandırdığı şey yıkıcı yarının kaldırılmasıdır. Gerçekten daraltmanın
    tek yolu deploy kimliğini `tappa-secrets` ile **aynı namespace'ten çıkarmaktır**
    (ayrı bir migration namespace'i) — yapılmadı, çünkü tek-node tek-namespace bir
    kurulumu ikiye bölmek yeni bir sınır kümesi doğurur.
17. **[`kubectl auth can-i` alt kaynakları GÖRMÜYOR]** — 🔴 **Ürünün değil, bir
    DOĞRULAMA ARACININ sınırı — ve tam da bu yüzden burada.** T44'ün kökündeki kusur
    `pods/exec`'in **ayrı bir alt kaynak** olması ve elle yazılmış rolde
    bulunmamasıydı; bunu kontrol etmenin en doğal yolu `kubectl auth can-i`'dir ve o
    yol **yanlış cevap veriyor** (ölçüldü, kubectl v1.36.1 / k3s v1.35.4, deploy
    SA'sını impersonate ederek): `can-i create pods/exec` → **yes** ·
    `can-i create pods/portforward` → **yes** · `can-i create pods/nonsense` →
    **yes** (kontrol — böyle bir alt kaynak yok), yani eğik çizgili biçim **ana
    kaynağa çöküyor**. Doğru biçim `can-i create pods --subresource=exec` → **no**,
    kontrolü `--subresource=nonsense` → **no**. **Bu ölçüm yüzünden `deploy.yml`'e
    bir `auth can-i` ön kontrolü EKLENMEDİ:** deploy'un sahip olmadığı bir yetki için
    *"yes"* diyebilen bir kapı fail-open bir kapıdır ve bu kart o sınıftan beş örnek
    üretti. Rolün gerçeğini `kubectl -n tappa get role github-deployer -o yaml` ya da
    `kubectl auth can-i --list -n tappa --as=system:serviceaccount:tappa:github-deployer`
    ile oku — ikincisi alt kaynakları **adıyla** listeler (`pods/log` görünür,
    `pods/exec` görünmüyorsa gerçekten yoktur).
18. **[sevk edilen commit'in kimliği herkese açık]** — ✅ **OPS-2 (2026-10-19) YENİ İMAJLAR
    İÇİN KAPANDI:** imajlar private Harbor'da, etiket listesi kimliksiz okunamıyor — deploy her
    koşuda anonim isteğin **reddedildiğini** ölçüyor (`verify-image.sh --access`). ⚠️ **Kalan:**
    OPS-2'den önce Docker Hub'a itilmiş `sha-*` etiketleri (`docker.io/atknatk/tappa`,
    `…/tappa-migrate`) depolar silinene ya da private yapılana dek **hâlâ herkese açık** — ama yeni
    bir sevkiyatın sinyali artık oraya düşmüyor. Silmek *"OPS-2 geri dönüş"*ün Docker Hub yolunu
    kapatır; bu yüzden ayrı bir karar. Aşağıdaki metin OPS-2 öncesinin kaydıdır.
    🔴 **§4.7 merceğiyle sayılan
    YENİ bir yüzey: ürün ikilileri artık herkesin indirebileceği bir yerde.** Karar
    operatör adımı 4'te ve `k8s/20-app.yaml`'da yazılıydı, **ama bu listede yoktu** —
    ve bu listenin varlık sebebi tam olarak budur.
    **ÖNCE NE AÇILMIYOR, ölçülerek (yani kaygının büyük kısmı sıfır):** depo
    (`atknatk/tappa`) **zaten public**, yani kaynak kodu açığa çıkarma **sıfır**;
    imajlar `scratch` üzerine kurulu ve içerikleri `Dockerfile`'ın son iki
    aşamasından birebir okunabiliyor — **uygulama imajı** = CA paketi + `/tappa`
    (statik ikili), **migration imajı** = CA paketi + `/goose` + `/migrations`
    (**20 `.sql`**, hepsi zaten public depoda). Sır yok; `deploy.yml` push'tan
    **önce** içeriği kapılıyor (`scripts/verify-image.sh` — tzdata, `vcs.revision`,
    goose'un uygulama imajında **bulunmaması**, non-root uid).
    ⚠️ **Adım 4'te bir tur boyunca *"imaj `scratch` + iki dosya"* yazdı ve bu iki
    artefaktın yalnız **birini** anlatıyordu**; migration imajı üç şey taşıyor.
    🔴 **AMA GERÇEK VE YENİ BİR DELTA VAR:** public bir Docker Hub deposunun **etiket
    listesi kimliksiz okunabilir** (kontrol ölçümü, `library/alpine` üzerinde:
    anonim registry token → `GET /v2/library/alpine/tags/list` **HTTP 200**;
    `hub.docker.com/v2/repositories/library/alpine/tags` → etiket adları düz döndü).
    Etiketlerimiz `sha-<12hex>` olduğu için **üretimde hangi commit'in koştuğu ve bir
    düzeltmenin sevk edilip edilmediği dışarıdan okunabilir hâle geliyor.**
    **Bunu başka hiçbir yüzey vermiyor** — ölçüldü: `buildinfo` yalnız sürecin
    log'una yazılıyor (`cmd/tappa/main.go:83`) ve
    `grep -rn buildinfo internal/handler web/templates` → **0**. Yani bu, M8-01'in
    *"derleme kimliği herkese açık bir uç noktaya çıkmıyor"* kararının **yeni bir
    kanaldan** delinmesidir.
    **Neden kabul edildi (OPS-2'ye kadar):** alternatifi private depo, o da KEP-2535 kimlik
    doğrulama kusurunu — yani **pod'un hiç açılamaması** riskini — geri getiriyordu; OPS-2 bunu
    kimliği **kimsenin yeniden yazmadığı** bir Secret'la çözdü (sınır **[Harbor k8s-2'ye bağlı]**). Açığa çıkan şey bir sır değil, bir
    **zamanlama sinyali**: saldırgan hangi commit'in canlıda olduğunu okur, kaynağı
    zaten okuyabildiği için. Takas bilinçli. **Kapatmak isteyen** için tek gerçek yol
    etiketi commit'ten ayırmaktır (ör. içeriğe göre türetilen opak bir etiket) — ama
    o zaman `sha-<12hex>`'in verdiği şey, yani *"kümedeki etiketten commit'i okuyup
    `git show` yapabilmek"*, kaybolur; olay müdahalesinde en çok kullanılan bağ budur.
19. **[yedeğin hedefi ve şifresi KÜME DIŞINDA yaşar — ve emanet edilmezse yedek yoktur]**
    — 🔴 **KAPATILAMAZ, SAYILIYOR (§4.7).** `k8s/50-backup.yaml` bilerek hiçbir
    sağlayıcı, kova ya da anahtar adı taşımıyor: hepsi operatörün yazdığı
    `secret/tappa-backup-target`'ın içinde (`rclone.conf` dosya olarak, `BACKUP_REMOTE`
    anahtar anahtar). **Bunun bedeli, bu reponun o hedef hakkında ölçemediği
    özelliklerdir** — erişilebilir mi, AB'de mi, ne kadar dayanıklı, kimin okuma
    yetkisi var: **dördü de burada doğrulanamadı** ve öyle yazılıyor.
    ✅ **BEŞİNCİ ÖZELLİK SAYILMIYOR, ÖLÇÜLÜYOR: hedef ŞİFRELİ Mİ.** Bu madde bir tur
    boyunca dört özellik sayıyordu ve *"şifreli mi"* aralarında yoktu — oysa öteki
    dördünden farklı olarak **o ölçülebilir**, hem de işin kendi içinden, koştuğu anda.
    Denetimin cümlesi: bu turun kendi ölçütü — *emanet **kanıtlanır**, beyan edilmez* —
    aynı muameleyi gerektiriyor.
    🔴 **VE İLK UYGULAMAM BUNU KARŞILAMIYORDU — düzeltildi, ve neden yanlış olduğu
    burada duruyor.** İlk hâli remote'un **`type` satırını okuyordu**; denetim sevk
    edilen imajla ölçtü ki **gerçekten `type = crypt`** olan bir remote'a tek bir
    seçenek eklemek yetiyor: `no_data_encryption = true` ile iş *"encrypted (crypt)
    remote"* deyip yüklüyor ve **exit 0** veriyordu, oysa hedefteki dump **düz gzip**
    ve manifest `cat` ile **okunabilir**. `filename_encryption = off` ise adları açığa
    çıkarıyordu. İkisi de `rclone config` sihirbazında **ayrı sorular**, yani tek yanlış
    tuşla oluşuyor.
    ✅ **Şimdi ölçülen şey BEYAN değil ÖZELLİK:** yüklemeden **önce** crypt remote'una
    bir kanarya yazılıyor ve **altındaki** remote'tan geri okunuyor — saklanan baytlar
    `RCLONE\0\0` ile başlamalı, saklanan **ad** düz metin markörü içermemeli. On beş
    yol ölçüldü ve **hepsi fail-closed**: varsayılan ✓ · `standard` ✓ · `obfuscate` ✓ ·
    `alias→crypt` ✓ · `no_data_encryption=true` ✗ · `filename_encryption=off` ✗ ·
    ikisi birden ✗ · **`TRUE` büyük harf ✗** · **boşluklu ` true ` ✗** · `remote=` yok
    ✗ · `alias→nodata` ✗ · düz `local` ✗ · tanımsız ✗ · bağlantı dizgisi ✗ ·
    on-the-fly `:local:` ✗. Büyük harf ve boşluklu varyantlar bu yaklaşımın gerekçesi:
    **özellik, seçeneğin nasıl yazıldığını umursamıyor.**
    ⚠️ **`filename_encryption = obfuscate` GEÇİYOR, ve bu SAYILMIŞ bir seçim** — bir
    gözden kaçma değil. rclone'un kendi belgesi obfuscate'i *"şifreleme değil, geri
    çevrilebilir bir karıştırma"* diye tanımlıyor: depoyu okuyabilen biri adları geri
    çevirebilir. Kontrol *"markör düz metin olarak görünüyor mu"* diye sorduğu için
    geçiyor. **Kalan maruziyet yalnız ADLARDIR** — yedek damgaları ve bu kurulumun
    yedekleme takvimi; **içerik** bayt kontrolüyle şifreli olduğu doğrulanıyor ve
    mesai kayıtları, GPS ve sarmalanmış AES anahtarları oradadır. Kabul edildi.
    ⚠️ **Ve kanaryanın `remote =` yazımına duyarlı olduğu ölçüldü:** yol taşımayan bir
    `remote = back:` (rclone sihirbazının **belgelediği** biçim, ve **SFTP** için doğal
    olan) ilk uygulamamda **her gece kırmızı** üretiyordu — kusursuz şifreleyen bir
    hedefte. Yedi yazım ölçüldü ve hepsi geçiyor: `back:` · `back:sub` · `back:sub/` ·
    `back:sub/deeper` · `/mutlak/yol` · `/mutlak/yol/` · alias arkasında.
    ⚠️ **Doğrulanamayan iki dal, açıkça:** kanaryanın silinmesi başarısız olursa ne
    olduğu (dal **uyarı basıyor** ama **üretilemedi**), ve SFTP'de mutlak/göreli
    ayrımının yerelde ölçülenle aynı doğup doğmadığı.
    ⚠️ *"Kanıtlanamadı"* ile *"şifresiz"* **ayrı iki mesaj**, ve ikisinde de hiçbir şey
    yüklenmiyor. Üç kanıtlanamama dalından ikisi tetiklendi; *"yazıldı ama alttan
    okunamadı"* dalı yerel bir backing store ile **üretilemedi → doğrulanamadı**.
    ⚠️ Tip sorgusu `rclone config show` ile yapılıyor, `config dump` ile **değil** —
    markörlü bir parolayla ölçüldü: `config show` **`password = *** ENCRYPTED ***`**
    basıyor (obscure edilmiş değer **0**, düz metin markör **0**), `config dump` ise
    obscure edilmiş değeri **2 kez** döküyor.
    🔴 **Şifreleme anahtarı `TAPPA_TAG_KEK` ile AYNI ZARFA KONMAZ, ve gerekçe
    simetrik değil:** `TAPPA_TAG_KEK` kaybolursa parktaki her plaket fiziksel olarak
    yeniden encode edilir ama **mesai kayıtları hâlâ okunabilir**; yedek şifresi
    kaybolursa **hiçbir şey** okunamaz — ve kaybın fark edileceği an, tam olarak
    kümenin gittiği andır. İkisi aynı kasada durabilir, **aynı zarfta duramaz**.
    ⚠️ **Ve emanet edilmemiş bir yedek anahtarı, yedeği olmayan bir sistemden daha
    kötüdür**: ikincisi bilinir, birincisi bilinmez.
20. **[yedek saklama süresi 30 gün — ve bu sayı B3'e bağlı, hukuki bir görüş değil]**
    — `50-backup.yaml` → `BACKUP_RETENTION_DAYS: "30"`, `pg-backup-ship.sh`
    `rclone delete --min-age` ile uyguluyor (7 günün altı **reddediliyor**; ölçüldü:
    `3` → exit 1 ve adıyla mesaj). 🔴 **Bir yedek, silinmiş bir kaydı canlı tutar**,
    yani bu sayı sınır **2**'nin (`TAPPA_RETENTION_YEARS=2`, hukukçu onayı bekliyor,
    backlog **B3**) çözülmemiş sorusuna bağlı: bir çalışanın silme talebi
    karşılandığında o kayıt **30 gün daha** yedeklerde yaşar. 30, *"hafta sonundan
    sonra fark edilen bir arızayı atlatacak kadar uzun, ikinci bir arşiv olmayacak
    kadar kısa"* diye seçildi. **Bu bir karar, ölçüm değil** — B3 kapandığında
    yeniden bakılmalı.
21. **[geri yükleme AYRICALIK ARTIĞI üretir — ağır yarısı YAPISAL olarak kapatıldı,
    kalanı prosedürle çevreleniyor]** — 🔴 **ÖLÇÜLDÜ, VE SESSİZ. VE İLK YAZIMI BU
    MADDENİN AĞIRLIĞINI EKSİK SAYDI — düzeltiliyor.** Taze bir Postgres pod'una
    yapılan düz bir `psql < dump` geri yüklemesi `tappa_app`'e **31 fazla yetki**
    veriyordu (kaynak **45**, geri yükleme **76**). Sebep: `01-roles.sql`'in
    `ALTER DEFAULT PRIVILEGES`'ı initdb'de koşuyor, geri yüklemedeki her `CREATE
    TABLE` dördünü de otomatik veriyor, ve `pg_dump` ACL'i **yerleşik** varsayılana
    göre hesapladığı için fazlalığı geri alan `REVOKE`'ları hiç yazmıyor. Dump doğru;
    hedef boş değil.
    🔴 **BU MADDE BİR TUR BOYUNCA *"yalnız §4.3, üstelik tetikleyici hâlâ reddediyor,
    ürün bozulmuyor"* DİYORDU. YANLIŞ — §4.4'e ve §4.7'ye uzanıyor.** Fazla 31'in
    ikisi **`tags` üzerinde tablo düzeyi UPDATE ve DELETE**, ve `tags`'te eşdeğer bir
    ikinci kemer **yok**: `tags_counter_monotonic` yalnız `BEFORE UPDATE … WHEN
    (new.last_ctr < old.last_ctr)` (ölçüldü, `pg_get_triggerdef`), yani **INSERT'te
    hiç ateşlemiyor**. Geri yüklenmiş gerçek şemada, `tappa_app` ile TCP üzerinden,
    henüz tap kaydı olmayan bir plakette (`transactions_tag_fk ON DELETE RESTRICT`
    sınırı):
    `ctr 5100` → `DELETE tags` **başarılı** → `INSERT … last_ctr 0` **başarılı** →
    `ctr 0` → **tekrar oynatılmış `ctr=7` kabul edildi**, ve aynı INSERT
    `aes_key_ref`'i `forged-key-ref` yaptı. **Yani §4.4'ün replay koruması çalınmış
    bir `tappa_app` kimliğiyle sıfırlanabiliyordu**, ve tablo düzeyi UPDATE sütun
    kısıtını ezdiği için **sarmalanmış AES anahtarları yazılabilir** hâle geliyordu
    (`has_column_privilege(tappa_app,tags,aes_key_ref,UPDATE)`: kaynakta **false**,
    geri yüklemede **true**).
    ✅ **AĞIR YARISI ARTIK YAPISAL OLARAK KAPALI.** `scripts/db-init/01-roles.sql`'in
    varsayılanı **`GRANT SELECT, INSERT`**'e daraltıldı. Daraltmanın taze bir kurulumu
    bozmadığı **gerçek goose imajıyla** ölçüldü: iki taze kurulum, 20 migration ikisinde
    de başarılı, **uygulama tablolarının tüm yetkileri birebir aynı (45 = 45)** — tek
    fark `goose_db_version` üzerindeki UPDATE+DELETE, yani goose'un kendi defteri.
    `go test -race -count=1 ./...` dar veritabanına karşı **exit 0, 22 paket, 0 FAIL,
    0 atlanan DB testi**. Aynı replay zinciri dar kurulumda **ilk adımda** duruyor:
    `ERROR: permission denied for table tags`.
    ❌ **KAPANMAYAN, SAYILIYOR:** daraltma sıfıra indirmiyor — taze bir pod'a geri
    yükleme hâlâ **5 fazla** yetki bırakıyor (`50` toplam), aralarında
    `legal_documents` üzerinde **tablo düzeyi SELECT**, ki o da kendi sütun listesini
    ezer. Sıfıra indiren şey **dar init + geri yüklemeden önceki askıya alma
    adımı**: ölçüldü, **45/0/0**. Bu yüzden askıya alma **B YOLU'nun 2. adımıdır**,
    bir dipnot değil. Ve `pg_dump`'ın davranışı elbette değişmedi: tek gerçek koruma
    `scripts/pg-restore-verify.sh`'i **her** geri yüklemede koşturmaktır — o da artık
    **tablo VE sütun** düzeyindeki yetkileri dump'ın ilan ettiğiyle karşılaştırıyor
    (45 tablo + 454 sütun) ve reddin `42501` olduğunu **assert ediyor**.
22. **[dar varsayılan ÜRETİMDE yürürlükte DEĞİL — ve doğrulama ortamı üretimden katı]**
    — 🔴 **BU MADDE `01-roles.sql`'İN KENDİ VAADİNİN KAPSAMINI SAYIYOR.** Dosya
    *"yeni bir tablo UPDATE/DELETE isterse açıkça GRANT etmek zorunda; unutulursa
    gürültülü `permission denied` alınır (fail-closed)"* diyor. **O vaat yalnız dar
    varsayılanın yürürlükte olduğu veritabanlarında geçerli**, ve iki yerde değil —
    ikisi de ölçüldü (2026-08-16):
    **(a) Çalışan veritabanları.** `01-roles.sql` bir **init script'idir**: yalnız boş
    `PGDATA` üzerinde koşar. Üretimin ve geliştirme veritabanının `pg_default_acl`'i
    bugün hâlâ **`tappa_app=arwd/tappa_owner`**.
    **(b) Her geri yüklemenin sonu.** `pg_dump`'ın ürettiği dump **kendi son
    satırlarında** varsayılanı yeniden genişletiyor
    (`GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO tappa_app`). Ölçüm dizisi: dar
    init → `ar` · geri yükleme sonrası → **`arwd`** · o anda yaratılan yeni tablo
    `tappa_app`'e **`arwd`** verir. **Var olan tablolar etkilenmiyor** (aynı ölçümde
    45 yetki, `tags` UPDATE/DELETE false) — etkilenen tek şey **sonraki DDL**.
    🔴 **NEDEN SAYILMASI GEREKEN BİR SINIR:** dev/CI **dar** idi, üretim ve her geri
    yüklenmiş veritabanı **geniş** — yani **doğrulama ortamı üretimden katıydı** ve ayrım
    kusuru **gizler** yönde. *(M11 API-1'den beri dev ve CI taze hacimde üretimin `arwd`'iyle
    kurulur: `scripts/db-init/03-dev-production-default-acl.sql`, ADR 0028 §1.)* `REVOKE` satırını unutan bir migration yerelde hiçbir fark
    üretmez, üretimde sessizce `arwd` verir. `pg-restore-verify.sh` bunu **göremez**:
    referansı dump'ın kendisi, ve dump geniş varsayılanı **ilan ediyor**.
    ✅ **Geri yükleme yolunda KAPATILDI:** A YOLU 7. adım ve B YOLU B3, dump'tan sonra
    varsayılanı tek ifadeyle geri daraltıyor (`REVOKE UPDATE, DELETE ON TABLES`),
    var olan tablolara dokunmadan — ölçüldü, `ar` geri geliyor ve yeni bir tablo
    `ar` alıyor.
    ❌ **ÇALIŞAN VERİTABANINDA KAPATILMADI, BİLİNÇLİ.** Aynı ifade üretimde de
    koşturulabilir ve **istege bağlı bir sertleştirme** olarak burada duruyor:
    ```bash
    kubectl -n tappa exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
    ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
      REVOKE UPDATE, DELETE ON TABLES FROM tappa_app;
    SQL
    ```
    Dayatılmıyor, çünkü canlı bir veritabanını migration dışında değiştirmek bir
    **karardır** ve bu ajanın değil. ⚠️ **Bugün gerçek bir açık DEĞİL:** 20
    migration'ın hepsi istemediğini açıkça `REVOKE` ediyor (**23 ifade**), yani hiçbiri
    varsayılana dayanmıyor. Bu bir **metin ve kapsam** meselesidir — ve bu kartın imza
    kusuru tam olarak odur: **sağlanmayan bir garantiyi ilan etmek.**

23. **[BU BELGEDEKİ ATIFLAR MEKANİK OLARAK DOĞRULANMIYOR — ve bu bir SAYIM, kapatma
    değil]** — 🔴 **Bu dosyadaki hiçbir atıf bir test tarafından okunmuyor:** ADR
    madde numaraları, NXP AN12196 bölüm/tablo/sayfa numaraları, migration numaraları,
    dosya ve sorgu adları, politika adları (`base:*`), durum değerleri (`unassigned`
    vb.) ve `open-questions.md` soru numaraları — hiçbiri. Aynısı
    `docs/plan/m8-deploy-pilot.md` için de geçerli. Ağaçtaki tek mekanik atıf kontrolü
    `cmd/tappa/testnames_test.go`'dur ve o **yalnız `Test` ile başlayan test adlarını**
    çözer; bir belge cümlesinin adını andığı ADR maddesinin var olup olmadığını
    **hiçbir şey** sormuyor.
    **Ölçüldü (M8-05, 3. tur denetimi).** Bu iki `.md` dosyasına sekiz uydurma
    yerleştirildi — var olmayan bir dosya adı · var olmayan bir politika adı
    (`base:qr-requires-gps`) · yanlış bir durum değeri (`status='active'`) · var
    olmayan bir doküman revizyonu (`rev. 9.9`) · bir GRANT listesine `aes_key_ref`
    eklenmesi · var olmayan bir ADR (`0099-hayali-adr.md`) · yanlış bir bayt uzunluğu
    (44 → 48) · yanlış bir madde sayısı. **Sekizinin sekizi de yakalanmadı:**
    `scripts/redline-check.sh` **rc=0**, dört Go paketi yeşil.
    ⚠️ **Sonuç neyi DEĞİŞTİRİR:** bu belgedeki bir atıf, onu **açıp okuyan** bir insanın
    ya da ajanın kontrolü kadar güvenilirdir. Bir gözden geçirme turunda atıfları
    örneklemek, testlerin yeşil olmasına güvenmekten daha bilgilendiricidir.
    ✅ **İkinci sıfır kapı KAPATILDI — T136 (OPS-1, 2026-10-18).** `cmd/rotatekek`'in tarayıcısı
    yapıştırılabilir blokların yorumlarını eskiden **dört dilimde** okuyordu ("KEK döndürme",
    "Plaket encode", "Gözlemlenebilirlik", operatör adımı 5); kalan blokların yorumlarında
    zsh'ta (`interactive_comments` kapalı) yan etki doğuran satırlar sayılmıştı — bir `#` satırı
    orada **`#` adlı bir komuttur**: içindeki `>`/`<` **dosya yaratır**, `;`/`|` **sonrasını
    koşturur** (gerçek `zsh -f -i`'de `# admin -> Settings -> Keys` çalışma dizininde `Settings`
    ve `Keys` dosyalarını yarattı), bir apostrof `quote>` açıp sonraki satırı yutar, satır içi
    yorumun kelimeleri komutun **argümanı** olur. **Ölçüm, değişiklikten önce:** dosyada
    **128** `bash` bloğu (dördü bir liste maddesinin içinde girintili — eski tarayıcının hiç
    görmediği — ve **dokuzu** bir `> ` alıntısının içinde), **79**'unda **258** yorum satırı ve
    **61** satır içi yorum; **32** blok `zsh -f -i -n` ile **ayrışmıyordu**. ⚠️ **İlk tur
    burada 119 / 71 / 244 / 56 / 31 yazıyordu ve YANLIŞTI:** çit kalıbı girintiyi tanıyor,
    alıntıyı tanımıyordu, yani alıntı içindeki dokuz blok (geri alma, yedek, kesinti teşhisi)
    hiç taranmadı ve **sekizi** yorumunu korudu — gerçek `zsh -f -i`'de biri apostrofla
    `quote>` açıyor, biri ikinci `curl`'ü (`/readyz`) hiç koşturmuyordu. Düzeltme turu
    (üçüncü gözün bulgusu) kalıba `> ` önekini öğretti ve testin kendisine *"alıntı içinde en
    az bir blok görüldü"* kontrolünü ekledi. **Çare — tek kural, iki kabukta aynı:** `bash`
    bloklarında **yorum yok**. Yorumlar blokların etrafındaki metne taşındı (açıklama paragraf,
    satıra özgü not bloğun önündeki listede, beklenen çıktı bir `text` bloğunda); yorumların
    durduğu yerlerden bloklar bölündü. **Komutlara dokunulmadı**, iki bağımsız yolla
    kanıtlandı: yorumlar atılınca eski ve yeni dosyanın komut satırları bayt bayt aynı
    (**544** = **544**), ve bash'in kendi ayrıştırıcısı (`declare -f`, yer tutucular
    nötrlenerek) **117** bölgenin **117**'sinde aynı programı veriyor. (Bu maddenin kendi ölçüm
    bloğu ayrıca **bilerek** değişti: `grep -c '^```bash'` yalnız sütun 0'daki çitleri sayıyordu,
    yerini testin logu aldı.) Düzeltme turu alıntı içindeki sekiz bloğun komutlarını da aynı
    iki yolla kanıtladı — bu kez alıntıyı tanıyan bir okuyucuyla, **bütün dosya** üstünde:
    yorumsuz komut akışı ve tüm blokları tek fonksiyonda toplayan `declare -f`, değişiklik
    öncesi dosyayla **tek** iki farkla aynı, ikisi de bilerek: bu maddenin ölçüm bloğu ve
    KEK döndürme adım 3'ün sonuna eklenen `rm -f "$PGPASSFILE"; unset PGPASSFILE` (etkileşimli
    kabukta `trap … EXIT` ancak kabuk kapanınca koşar). **Sonra:** **167** `bash` bloğu (4'ü
    girintili, 10'u alıntı içinde), **0** yorum, **167**'sinin **167**'si `zsh -f -i -n` ile
    ayrışıyor (yer tutucular `<…>` nötrlenerek). Tutan test `TestRunbook_EveryPasteableBlockCarriesNoComment`
    (bütün dosya, girintili ve alıntı içindeki bloklar dahil; `bash` dışındaki çitler — çıplak, `text`, `sql`,
    `ini` — kabuğa yapıştırılmaz ve tutulmaz). Sayıyı yeniden üretmek için testin kendi
    logu:

    ````bash
    go test ./cmd/rotatekek/ -run 'PasteableBlock' -v
    ````

24. **[log saklama süresi: node'da BOYUT, toplayıcıda BİLİNMİYOR]** — M8-03, ölçüldü.
    ✅ **Bilinen:** node kopyası `containerLogMaxSize=10Mi` × `containerLogMaxFiles=5`
    = konteyner başına **50 MiB**, ve her deploy öncekinin log'unu atar (7 ReplicaSet,
    **1** pod). ❌ **Bilinmeyen:** SigNoz/ClickHouse kopyasının TTL'i — okumak `exec`
    ya da SigNoz API'si ister, bu turda ikisi de yapılmadı. **Varsayılana güvenilmedi,
    sayı yazılmadı.** Kapatan eylem `log-retention-signoz` (yukarıda) ve kararın
    kendisi `Q28 (b)` — **saklama** yarısı. 🔴 **M8-06 pilot
    kapısı:** log satırları IP ve çalışan id'si taşır ve Q13'ün silme akışı onlara
    ulaşmaz, yani bilinmeyen bir süre GDPR Art. 13 metnindeki sayıyı yalanlayabilir.
    ⚠️ `Q12` (barındırma) bu maddeye **dolaylı** bağlıdır: nerede barındığımız node
    kopyasının rotasyon ayarını ve toplayıcının varlığını belirler. Ama *"ne kadar
    saklanacak"* sorusu `Q28 (b)`'dir, `Q12` değil.
25. **[uyarıların teslimatı: ~~kanal yok~~ → kümenin İÇİNDEN, tek kontrol üzerinden]** —
    🔴 **T118 (2026-10-11) — TESLİMAT KURULDU, kör noktaları sayılı.** Yedi kural kümedeki
    SigNoz'da ClickHouse-SQL eşik kuralı olarak koşuyor (kaynak
    `deploy/observability/signoz-alert-rules.json`, `TestSignozRules_` testleri) ve
    `tappa-healthchecks` webhook'u → healthchecks.io `tappa-signals` → Telegram + e-posta
    üzerinden gidiyor (operatör adımı 10 (f); *"M8-03 — UYARI KURALLARI"* kutusu).
    ❌ **Kapanmayan, sayılı:** (a) **SigNoz'un tek başına ölmesi sessizdir** — SigNoz
    pod'u, ClickHouse ya da log toplayan ajan durursa kurallar değerlendirilmez ve hiçbir
    şey bunu söylemez; `tappa-signals`'ın toleransı 365 gün (ölü adam anahtarı değil).
    Kümenin/node'un ölümü ise `tappa-uptime`'a düşer (sınır 33). (b) **Kurulu kural
    tablodan dardır** — beş basitleştirme kutuda adıyla (1, 2, 4, 5, 7), ve pencereler kayan
    değil saate hizalı kovalar olduğundan 1., 2. ve 5. kural sınırda bölünen bir patlamayı
    kaçırabilir. (c) **Tek kontrol,
    bütün kurallar:** bir kuralın `resolved`'ı kontrolü `up`'a çeker, başka bir kural hâlâ
    ateşteyken bile; kontrol `down` iken yeni bir kural yeni bildirim üretmez (tasarımın
    sonucu, ölçülmedi). (d) Canlı kanalda `send_resolved` **açık** (orkestratör ölçtü, 2026-10-11;
    kanal yapılandırması API'den okundu, adres basılmadan). (e) Tek sağlayıcı — sınır 33 (a) aynen.
    **Aşağısı 2026-10-11 öncesinin kaydıdır, tarih olarak duruyor:**
    M8-03'ün üçüncü kriteri **hesaplanabilirlik**
    olarak sevk edildi: **altı** sinyalin altısı da log'dan bir filtre ile çıkıyor, alan
    adları `TestObservability_AlertSignalNames` ile, filtrelerdeki politika adları
    `TestObservability_EverySidInTheAlertRulesExists` ile pinlendi, eşikler yazılı.
    **Hiçbiri bir yere gönderilmiyor** — hedef `Q28 (a)`'da (**teslimat**) açık ve
    kurulum bu repodaki bir dosyada değil. Sahibi: kümeyi işleten operatör.
    ⚠️ Bu madde bir tur boyunca `Q12`'ye atıf veriyordu; `Q12` **barındırmadır** ve
    uyarı hedefi hakkında hiçbir şey söylemez. Sorunun kendisi (`Q28`) o yanlış atıf
    ölçülünce açıldı.
    ⚠️ **T116 (2026-10-09) — DAR bir parça kapandı, bu madde KAPANMADI.** Log **dışı** iki
    sinyal artık kümenin dışına gidiyor: halka açık `/healthz` (kümenin ölümü dahil) ve
    gecelik yedeğin sonucu, healthchecks.io üzerinden e-postayla (operatör adımı 10, sınır
    33). M8-03 tablosunun log sinyalleri — bugün **yedi**; bu maddenin *"altı"*sı OP-8'in
    yedinci kuralından önce yazıldı — hâlâ teslimatsız.
26. **[request id korelasyonu TAP ZİNCİRİYLE SINIRLI]** — M8-03. `slog.Handler`
    sarmalayıcısı yalnız `*Context` çağrılarını damgalayabilir.
    🔴 **HANGİ AĞACIN SAYISI OLDUĞU HER SAYININ YANINDA YAZIYOR** — bir tur bu maddede
    **346**, `internal/httpx/requestlog.go` içinde **347** yazdı ve aynı paragraf
    `323 = 346 − 23` hesabını çelişkiye düştüğü sayıdan türetti. Yeniden ölçüldü (bu
    reponun kendi tarama kurallarını taklit eden bir AST yürüyüşüyle;
    `cmd/tappa/observability_test.go`):
    **KART ÖNCESİ ağaç (kararın verildiği ağaç): 346** çağrı yeri — **51**'i `ctx`
    taşıyan bir fonksiyonun içinde, **272**'si yalnız `*http.Request` taşıyanın,
    **23**'ü hiçbirinin (başlangıç/arka plan satırları; **hiçbir şekilde** korele
    edilemezler) · 46 dosya · **0** `*Context` çağrısı.
    **KART SONRASI ağaç: 349** — **54** · **272** · **23** · 47 dosya · **32**
    `*Context` çağrısı.
    ⚠️ **BU İKİ SAYI 2. TURDA YANLIŞTI** (`53 · 273` yazıyordu) ve yanlışlığın yönü
    tesadüf değildi: ağaç **3** çağrı yeri kazandı ve **üçü de** `ctx` taşıyan bir
    fonksiyonun içinde. Üçü **adlarıyla** (satır numarasıyla değil; bu dosyanın
    `deploy.yml` dersi): `internal/domain/checkin`'de `Service.Record`'un
    `s.log.WarnContext(ctx, EventTapSecurityAlert, …)` satırı · yine `Service.Record`'un
    `s.log.Log(ctx, decisionLevel(…), EventTapDecision, …)` satırı ·
    `internal/httpx`'te `writeAccessRecord`'un `log.LogAttrs(ctx, …, EventHTTPRequest, …)`
    satırı (her yanıt için bir `http.request` kaydı). Yalnız `*http.Request` taşıyan
    kova **hiç değişemez** (o üç fonksiyonun hiçbiri o şekilde değil), yani doğru
    hesap `51 + 3 = 54` ve `272` sabittir.
    ⚠️ **5. TURDA DÜZELTİLDİ — bu üçü bir tur boyunca ÜÇ GEÇERSİZ SATIR NUMARASIYLA
    (`checkin.go:800` · `:823` · `requestlog.go:301`) ve `Log` önce gelecek şekilde
    yazılıydı; gerçek sıra kaynakta `WarnContext` → `Log`'dur.** Yani belge, tam
    olarak **kaldırılmış** düzeni belgeliyordu. Satır numarası bu dosyada defalarca
    bayatladı; artık sembol adı yazılıyor.
    **`*Context` sonekli 32 çağrının dağılımı** — `internal/handler/tap.go` 9 ·
    `internal/handler/checkin.go` 9 · `internal/domain/checkin/policyset.go` 8 ·
    `internal/domain/checkin/checkin.go` 5 · `internal/handler/health.go` **1**.
    Bunların **31'i var olan bir çağrının dönüştürülmesidir** (`Error` →
    `ErrorContext` vb.: tap.go 9 · handler/checkin.go 9 · policyset.go 8 ·
    domain/checkin.go 4 · health.go 1), **1'i yenidir** (`Service.Record`'un
    `EventTapSecurityAlert` satırı). `31 + 1 = 32`.
    ⚠️ **[GERİ ÇEKİLDİ — bu paragraf iki tur boyunca 33 dedi ve ikisinde de aynı
    sebeple yanlıştı.]** Önceki metin *"dönüştürülen 32 + 1 erişim kaydı"* ile ölçülen
    `*Context` sayısını eşitliyordu; 3. tur bunu *"tek bir 33"*e indirerek düzelttiğini
    yazdı ama **aynı hatayı tekrarladı** — dökümü `internal/handler/health.go`'ya **2**
    veriyordu, oysa o dosyadaki iki kayıttan biri `LogAttrs`'tır ve `LogAttrs`
    `*Context` **değildir** (`EventReadinessLost`; `*Context` olan tek çağrı
    `EventReadinessRegained`). Erişim kaydı da (`writeAccessRecord`) aynı sebeple
    sayının dışındadır. Ölçüm (AST, `cmd/tappa/observability_test.go`'nin kapsamıyla):
    **32**. Doğru sayı `31 + 1`'dir ve `LogAttrs` hiçbir yarıya girmez.
    Geri kalanı backlog **T51**'in ölçtüğü paket çapında dönüşümdür
    (`internal/handler`'da 224 çağrı yeri) ve bir kartın işi değildir.
    `request_id` taşımayan bir satır bozuk değil, **erişilemez**.
    ⚠️ Dönüştürülmeyen **bir** çağrı bilinçli: `policyset.go`'nun `OnAnomaly`
    geri çağrısı `tap.Decide`'ın içinden çağrılıyor ve `tap.Decide` **saf**
    (§5) — oraya bir `context.Context` sokmak ürünün karar çekirdeğinin imzasını
    bir log satırı için değiştirmek olurdu.
27. **[`slog.Default()` 43 yerde okunuyor — ve 20'si `cmd/tappa` DIŞINDA]** — M8-03,
    yeniden sayıldı. `cmd/tappa/main.go`'daki sarmalama yorumu bir tur boyunca
    *"aşağıdaki **44** `slog.Default()` okuması"* diyordu ve **her iki yarısı da
    yanlıştı**: dosyada **23** gerçek okuma var ve **hepsi** `SetDefault` satırının
    altında. ⚠️ Çıplak `grep -c 'slog\.Default()' cmd/tappa/main.go` bugün **27**
    der; aradaki **4** satır **yorumdur** ve üçünü bu kart ekledi — yani bu sayı
    canlıdır, kart eklendikçe büyür. Bir tur burada **26** yazdı ve o rakam
    yorumların eklendiği turda zaten bayatlamıştı; sayı yerine **ölçüm komutu**
    yazmanın sebebi bu. **Dışarıda 20 tane daha var** ve
    hepsi bir yapıcıdaki `if log == nil { log = slog.Default() }` geri düşüşü —
    `internal/domain` 12, `internal/handler` 7, `internal/httpx` 1. Toplam **43**.
    ⚠️ **Bu bir §7 ihlali DEĞİL ve bu turun getirdiği bir şey değil:** geri düşüşler
    kartlardan önce vardı ve enjekte edilen logger'ı ezmiyor, yalnızca `nil`
    geçildiğinde devreye giriyor. Sayı buraya, argümanın doğru zemine oturması için
    yazılıyor: sarmalama **varsayılan handler'a** yapıldığı için o 20 yol da
    `request_id` damgasını aynı kapıdan alıyor. **SAYILDI, kapatılmadı** — geri
    düşüşleri kaldırmak (ve her yapıcıyı zorunlu bir logger'a bağlamak) ayrı bir iş.
28. **[sağlık sondaları erişim kaydının DIŞINDA — kayıt yer değiştirdi]** — M8-03
    2. tur. `/healthz` → 200 ve `/readyz` → 200/503 için `http.request` kaydı
    **yazılmıyor** (gerekçe ve üç ölçüm: *"SAĞLIK SONDALARI"* bölümü). Hazırlık
    arızası `readiness.lost` / `readiness.regained` ile, geçiş başına bir kayıt
    olarak görünür kalıyor ve 6. kural onu okuyor. ⚠️ **Ne kaybedildi, açıkça:**
    `/readyz`'in **kaç kez** 503 döndüğü artık log'dan sayılamaz — yalnız **ne zaman
    başlayıp ne zaman bittiği** bilinir. Bu bilinçli: sayının kendisi sonda periyodunun
    yeniden söylenmesinden ibaretti.
    🔴 **TAKASIN İKİNCİ YARISI, ÖLÇÜLDÜ: `readiness.lost` "daha iyi" DEĞİL, DAHA
    DAR — bir kapsam takasıdır.** **Kalıcı** bir arıza kaç istek sürerse sürsün
    **tam 1** kayıt üretir: `TestReadyz_AnOutageCostsTwoLogLines` **200** başarısız
    sondaya karşı **tam 1** `ERROR` satırı olduğunu koşuda doğruluyor. Yani sinyal
    olayın **başladığı ana** bağlıdır, süresine değil. İki somut sonucu var. **(a)** Operatörün sorgu
    penceresinden **önce** başlamış bir arıza o pencerede **görünmez** — devam eden
    kesinti, sessiz bir sistemden ayırt edilemez. **(b)** O tek satır rotasyonla
    düşerse (madde 24: node kopyası boyut sınırlı, üst sınır bir sonraki deploy)
    sinyal **tamamen** yok olur; 12 satır/dk'lık eski kayıtta bunun için tüm
    pencerenin dolması gerekirdi.
    ⚠️ **Karşı örnek aynı repoda ve bilinçli olarak farklı davranıyor:**
    `announceKEKRotationWindow` (`cmd/tappa/main.go`) tam bu sebeple **15 dakikada
    bir tekrarlıyor** — açık bir KEK penceresi devam eden bir durumdur ve tek bir
    açılış satırı onu göstermez. `readiness.lost` bugün **tekrarlamıyor**. Tekrar
    eklemek yeni bir davranıştır ve bu turun kapsamı dışıdır; **sayılmamış bir
    sessizlik bırakmamak** için burada yazıyor.
    ⚠️ Ve `/healthz` için **hiçbir** kayıt yok; o uç
    noktanın arızası yalnızca kubelet tarafında görünür (`kubectl -n tappa describe
    pod`, `RESTARTS`).
29. **[log hedefi panikleyince ERİŞİM KAYDI DÜŞÜYOR — ve düştüğü söyleniyor]** — M8-03
    2. tur. `httpx.AccessLog`, `middleware.Recoverer`'ın **dışında** duruyor (bir 5xx'i
    görebilmesi için) — yani kaydı yazan `slog.Handler` **paniklerse** onu aşağıda
    kurtaracak bir ağ yok ve `writeAccessRecord` kendi `recover`'ını taşıyor. **Bu
    kurtarma bir kaybı gizler:** o istek için `http.request` kaydı **yazılmadı ve
    yeniden denenmiyor**. İkinci bir hedef **yok** — panikleyen şey sürecin log'unun
    ta kendisi.
    ⚠️ **§4.6 muhasebesi, tam:** kayıt sessizce atılmıyor, `stderr`'e bir uyarı
    basılıyor; ve o uyarı **panik değerini basmıyor** (yarı biçimlenmiş bir kaydın
    içeriğini taşıyabilir — §4.7). ⚠️ **Uyarının kapsamı SÜREÇ BAŞINA DEĞİL,
    `AccessLog` KURULUMU BAŞINA:** ölçüldü, tek süreçte iki kurulum uyarıyı **iki
    kez** basıyor. Süreç başına tek `sync.Once` bir paket seviyesi singleton olurdu
    ve §7 onu yasaklıyor; `cmd/tappa` tek router mount ettiği için bu ikili pratikte
    çakışıyor, ama bu **çağıranın** özelliğidir ve öyle yazılıyor.
    `TestAccessLog_ADroppedRecordIsAnnouncedOnce` iki yarıyı da ölçüyor. **SAYILDI,
    kapatılmadı** — kaydı bir yere kuyruklayıp yeniden denemek, tam da bu maddenin
    reddettiği ikinci bir log yolu inşa etmek olurdu.
    ⚠️ Bu maddenin **diğer yarısı** (tasarlanmış sonda cevaplarının hiç
    kaydedilmemesi) **28**'dedir; ikisi aynı takasın iki ucudur ve bir tur boyunca
    yalnız biri yazılıydı.
30. **[`httpx.NewRouter`'a `nil` logger vermek ERİŞİM KAYDINI SESSİZCE KAPATIR]** —
    M8-03, 5. tur, ölçüldü. `AccessLog(log)` ilk iş olarak `if log == nil { return next }`
    yapıyor: `nil` verilen bir router **hiç** `http.request` kaydı yazmaz, hiçbir uyarı
    basmaz ve `NewRouter` yine sorunsuz döner. Bugün **doğru**, ama **kaza eseri
    doğru**: üretimde tek çağıran var (`cmd/tappa`, `slog.Default()` veriyor) ve
    geri kalan çağıranların hepsi test. Yapısal bir engel **yok** — imza `nil`'i
    kabul ediyor, derleyici bir şey demiyor, ve kaybolan şey **5. uyarı kuralının
    tek girdisi** (5xx oranı). Davranışın kendisi `TestAccessLog_NilLoggerIsSilent`
    ile **pinli** — yani sessizlik kaza değil, sözleşme; sayılan şey sözleşmenin
    **yanlışlıkla** tetiklenebilir olması. ⚠️ **Kapatmak yeni davranıştır** (panik, zorunlu
    parametre ya da sessiz `slog.Default()` geri düşüşü — üçü de ayrı bir karar) ve
    bu turun kapsamı dışında; **sayıldı, kapatılmadı**. Not: `nil`'i kabul etmek
    testler için bilinçli bir kolaylıktı ve bir router'ı sessize almanın **tek**
    yolu bu — yani riskin adı *"unutulmuş argüman"*tır, kötü niyet değil.
31. **[`/readyz` durum değişimini `h.mu` TUTARKEN logluyor]** — M8-03, 5. tur,
    ölçüldü. `internal/handler.Health`'in `check` fonksiyonu kilidi sonda boyunca
    tutuyor (bilinçli: eşzamanlı çağıranlar tek bir sorgunun arkasına diziliyor) ve
    `EventReadinessLost` / `EventReadinessRegained` kayıtları **o kilidin içinde**
    yazılıyor. Tıkanan bir log hedefi, **yalnız durum değiştiği anda**, `/readyz`'i
    kilit süresince **bloklar**. ⚠️ **ŞEKİL BU TURUN GETİRDİĞİ DEĞİL** — kilit de,
    kilit altındaki kayıt da kart öncesinden geliyor; bu turda değişen tek şey
    satırların **adlandırılmasıydı**. Buraya yazılmasının sebebi bir tutarsızlık:
    `/healthz`'in **aynı** tehlikesi (log borusuna bağlanmış sonda) **28**'de ve
    *"SAĞLIK SONDALARI"* bölümünde ölçümüyle yazılıyken, iki dosya ötedeki bu ikizi
    hiçbir yerde sayılmıyordu. ⚠️ Fark, tehlikenin **sıklığında**: `/healthz`
    her sondada yazıyordu (o yüzden kaldırıldı), bu yalnız **geçişte** yazıyor — yani
    dar, ama sıfır değil. Kaydı kilidin dışına almak yeni davranıştır (kayıt sırası
    ve `h.failing` okumasının atomikliği değişir) ve bu turun kapsamı dışındadır;
    **sayıldı, kapatılmadı**.
32. **[kapalı/ulaşılamaz operatör yüzeyinde `/operator` istekleri İZ BIRAKMIYOR]** — M10
    OP-7, 2c–2d. Yüzey kapalıyken ya da ulaşılamazken `/operator` ve altı her istekte 503
    verir ve bu cevap **tasarımdır**: `httpx.AnswerAsDesigned` ile bildirilir ve
    `http.request` kaydı **yazılmaz** (28'in emsali; gerekçe *"SAĞLIK SONDALARI"*
    bölümündeki `/operator` paragrafı). ⚠️ **Ne kaybedildi, açıkça:** (a) bu iki hâlde
    `/operator`'a yapılan **denemeler süreç log'unda iz bırakmaz** — bir tarayıcının
    operatör yolunu yoklaması buradan görülmez; (b) **"kayıt yok" ile "istek yok" ayırt
    edilemez** — bir tarama yalnız **ingress'in** erişim log'unda görünür. Durumun kendisi
    görünür kalır: açılış satırı (`"operator_surface"`), kapalıysa INFO, ulaşılamazsa
    **ERROR**. ⚠️ **O ERROR satırını okuyan bir uyarı kuralı YOK:** ulaşılamaz bir yüzey
    bugün yalnız rollout sonrası runbook sorgusuyla (`grep -c '"operator_surface":"unavailable"'`)
    görülür. Öneri (6. kuralın emsali, **eklenmedi**): `body.operator_surface = "unavailable"`
    → **≥ 1** olay = uyarı, pencere yok; kapanışı bir sonraki açılışın `configured` satırıdır.
    Yüzeyin canlıda açılacağı OP-8'e devredildi (m10-platform.md, OP-7 kart düzeltmesi
    md. 9 (ix)). ~~**Sayıldı, kapatılmadı.**~~ → **M10 OP-8 (2026-10-02): EKLENDİ — uyarı
    kurallarının 7. satırı** (yukarıdaki tablo ve yapıştırılabilir blok;
    `TestObservability_AlertSignalNames` alan adını ve değeri koddaki sabitlere bağlar). Gerekçe:
    OP-8 yüzeye gerçek rotalar verdi, yani `unavailable` artık "henüz bir şey sunmayan bir
    yüzeyin" değil, **operatörün kilitli kaldığı bir konsolun** hâlidir ve satır süreç
    açılışında bir kez yazılır — okunmazsa başka bir sinyal onu taşımaz (yedi kuralın
    tablosu). Kalan, adıyla: (a) ve (b) aynen geçerlidir
    (kapalı/ulaşılamaz yüzeyde `/operator` istekleri iz bırakmaz); kural satırı açılıştan sonra
    döndürülmüş bir log'da göremez (6. kuralın daralması). Yapılandırılmış yüzeyin kendi 503'leri
    (giriş sırasında bir veritabanı arızası) tasarım DEĞİLDİR ve 5. kuralda kayıtlıdır.
33. **[dış ölü adam anahtarı: tek sağlayıcı, küme içinden yoklama, yedek sinyalinin adresi
    argv'de]** — T116, 2026-10-09; operatör adımı 10. ✅ **Kapanan:** node'un/kümenin
    ölümü, halka açık `/healthz`'in 200 dönmemesi (TLS, DNS, ingress ve — hazırlığı düşen
    pod Service'ten çıktığı için — veritabanı dahil) ve gecelik yedeğin sonucu artık
    kümenin **dışından** e-postayla bildiriliyor. ❌ **Kapanmayan, sayılı:**
    (a) **Tek sağlayıcı, tek kanal, tek alıcı.** healthchecks.io'nun kendisi, e-posta
    teslimi ya da hesap sahibinin gelen kutusu düşerse uyarı yoktur ve bu repodan
    izlenemez. Ücretsiz planın kotası ve SLA'sı **ölçülmedi**.
    (b) **Yoklama küme İÇİNDEN başlar.** Pod `taptime.mt`'yi kümenin DNS'iyle çözer ve
    node'un kendi genel adresine bağlanır (hairpin); node'un **dışındaki** bir
    yönlendirme ya da güvenlik duvarı arızası bu yoklamayı geçer. Hairpin'in bu kümede
    **çalıştığı ölçüldü** (orkestratör, 2026-10-09: `restricted` bir pod'dan
    `https://taptime.mt/healthz` → 200, uzak adres `144.76.158.60`, 0,028 sn) — bu bir
    andır, bir güvence değil; değişirse seçenekler (orkestratörün): yoklamayı küme içi
    Service'e çevirmek (TLS/DNS/ingress kapsamı düşer) ya da dış bir yoklayıcı.
    (c) **Yedek sinyalinin adresi kısa bir an argv'de.** `rclone/rclone:1.71`'de curl yok;
    busybox wget adresi yalnız argv'den alır — ölçüldü: wget çalışırken adres
    `/proc/<pid>/cmdline`'da. Pencere: gecede bir kez, en çok ~12 sn ölçüldü (`-T 10`;
    `timeout 30` yedek fren). Okuyucular: aynı konteynerdeki süreçler (yalnız betiğin
    kendisi; pod'da `shareProcessNamespace` yok — `TestCronJobs_PodsAreLockedDown`) ve
    node düzeyi (root ve host PID ad alanındaki ajanlar). **Somut aday bu belgenin
    kendisinde:** SigNoz'un `k8s-infra-otel-agent` DaemonSet'i bu node'da koşuyor
    (*"Gözlemlenebilirlik — M8-03"* → *"Log nereden çıkar"* tablosu ve oradaki komut
    bloğu). O ajanın `hostmetrics` alıcısında **`process` scraper'ı** açıksa her sürecin
    `process.command_line`'ını toplar ve ClickHouse'a yazar — o zaman bu pencereye denk
    gelen bir kazıma adresi SigNoz'a taşır. Açık olup olmadığı **ölçülmedi**; ölçümü
    o bölümdeki komutun eşidir, `filelog` yerine `hostmetrics` aranarak:
    `kubectl -n signoz get cm k8s-infra-otel-agent -o jsonpath='{.data.otel-agent-config\.yaml}' | grep -A 20 'hostmetrics'`
    — `scrapers:` altında `process:` yoksa aday düşer. Sinyali başka bir imaja ya da
    konteynere taşımak yedeğin sonucunu o imajın çekilebilmesine bağlardı — reddedilen
    takas. Heartbeat'te kapalı: curl adresi stdin'den okur (`-K -`, ölçüldü). Her iki
    betik de `sh -x` ile koşturulsa bile adresi basmaz: sinyal kodunun ilk satırı
    izlemeyi kapatır (ölçüldü — korumasız hâlde üç-dört iz satırı adresi taşıyordu;
    `TestHeartbeat_XtraceCannotPrintTheSignalURL`, `TestBackupShip_XtraceCannotPrintTheSignalURL`).
    (d) **`curlimages/curl:8.22.0` artık DIGEST'le sabit** (`@sha256:58adaa4e…6777`, çok
    mimarili index — yerel depoda ortam türü `manifest.list.v2` okundu, `tag@digest` ile
    çekilip koşturuldu; `TestHeartbeat_ImageIsPinnedToAnExactVersion` 64 hanenin tamamını
    ister). Kalan: Docker Hub anonim çekme bütçesinden (bölüm 2'nin `429` satırı) bir imaj daha harcar;
    `IfNotPresent` ile node başına bir kez. Yükseltme = yeni etiket **ve** yeni digest, tek
    düzenlemede.
    (e) ~~**`Q28 (a)`'nın KALANI:** M8-03'ün yedi log sinyali hâlâ hiçbir yere
    gönderilmiyor (sınır 25).~~ → **T118 (2026-10-11):** yedi log sinyali de aynı
    sağlayıcıya, `tappa-signals` üzerinden gidiyor; kör noktaları sınır 25'te. Bu madde
    yalnız log **dışı** iki sinyali anlatır. `Q28 (b)` (log saklama) açık (sınır 24).
    ⚠️ **T118 DÜZELTMESİ — bu madde ve adım 10 *"e-postayla"* diyordu, ve iki gün boyunca
    DOĞRU DEĞİLDİ.** Ölçüldü (orkestratör, 2026-10-11): API ile kurulan bir kontrol hiçbir
    bildirim kanalına bağlanmaz; `tappa-uptime` ve `tappa-backup` yalnız Telegram'a bağlıydı,
    e-posta entegrasyonu **hiçbirine** bağlı değildi. Bugün üçü de `"channels": "*"`:
    Telegram + e-posta. (a)'daki *"tek kanal"* bu yüzden artık *"iki kanal, tek sağlayıcı,
    tek hesap"*tır (adım 10 (f), tuzak 1).
    (f) **macOS'un bash 3.2'si** (geliştirici makinesinin `/bin/sh`'ı), EXIT tuzağı kurulu
    bir betik `${X:?}` ile bittiğinde tuzakta `$?`'yi 0 görür ve **0 ile çıkar** —
    ölçüldü (dash 2, busybox 2, bash 5 1). `pg-backup-ship.sh`'ın çıkış kodu bundan T116'dan
    önce de etkileniyordu ve üretimi (busybox) etkilemez; sinyal bu yüzden yalnız çıkış
    koduna değil betiğin son satırına ulaşılmasına da bağlı (`ship_complete`,
    `TestBackupShip_TheSignalNeverChangesTheOutcome`).
34. **[deploy tailnet'e üçüncü-parti bir action ve bir OAuth client'la girer]** — T134,
    2026-10-17; OPS-2 (2026-10-19: auth key → OAuth client, `production` ortamı); operatör adımı
    5 (a). *(Eski adı: [deploy tailnet'e üçüncü-parti bir action ve süreli bir auth key'le girer].)* Küme API'si internete kapalı; deploy `10.43.0.1:443`'e
    tailnet üzerinden ulaşır. ✅ **Kapanan:** genel `:6443` artık deploy'un yolu değil;
    runner'ın neyle katıldığı **ölçülüyor** (`tag:ci` yoksa kırmızı) ve API'ye ulaşamazsa
    kümeye hiçbir şey uygulanmadan duruyor; kubeconfig'in sunucusu `10.43.0.1`'e bağlı, bir
    `proxy-url` ya da `insecure-skip-tls-verify: true` taşıyamaz. İki kapı da
    `scripts/verify-deployment.sh`'in kipleridir ve testleri onları **koşar**.
    ❌ **Kapanmayan, sayılı:**
    (a) **Depodaki tek üçüncü-parti action.** `tailscale/github-action` tam commit SHA'sıyla
    pinli (`TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet` etiketi
    reddeder), ama pinli kod Tailscale'in ikilisini **çalışma anında indirir**. ✅ **T135
    (2026-10-18):** `version: "1.94.2"` + `sha256sum` pinli — action beklenen digest'i artık
    aynı sunucudan (`pkgs.tailscale.com`) almıyor, `deploy.yml`'dekiyle karşılaştırıyor
    (`KUBECTL_SHA256` gibi); test digest'i değeriyle tutar. ⚠️ **Kalan, sayılı:** pinli digest
    de o sunucudan **bir kez** ölçüldü (indirilen arşivin yerel hash'i = yayımlanan `.sha256`);
    o andan **sonraki** bir değişikliği yakalar, o an zaten değiştirilmiş bir dosyayı değil
    (ilk-kullanımda-güven). Tailscale'in paket imzası doğrulanmıyor.
    (b) **OAuth secret'ı `tailscale up`'ın argv'sinde VE `tailscaled`'in ortamında.** Action
    OAuth secret'ını `--authkey=<secret>?preauthorized=true&ephemeral=true` olarak geçirir (OPS-2; auth
    key'de de aynı yoldu — `dist/index.js`, koşan kod; kaynağı `src/main.ts` aynı): `up` koşarken runner'ın süreç tablosunda
    görünür, ve zaman aşımına uğrayan bir `up` **öldürülmez** (action yalnız beklemeyi bırakır).
    Ayrıca `tailscaled` `sudo -E` ile, action'ın ortamında `INPUT_*` girdileri dururken başlatılır:
    değer daemon'un `/proc/<pid>/environ`'unda **işin sonuna kadar** — post adımının
    `pkill`'ine dek — kalır. Okuyabilen: root, yani bu işin kendi sonraki adımları (runner'da
    şifresiz `sudo` var). Okuyucu kümesi argv'ninkiyle **aynı** — GitHub-hosted runner tek
    işlik bir VM — bu yüzden **kabul edildi**; değer log'da maskelenir (runner her `secrets.*`
    değerini maskeler) ve `log-mode: quiet` action'ın komutlarını sessiz koşturur.
    Self-hosted runner'a geçilirse bu madde yeniden açılır. ⚠️ **OPS-2 ile ağırlaştı:** sızan bir
    auth key bir cihaz katabilirdi ve ≤ 90 günde ölürdü; sızan bir OAuth secret'ı **iptal edilene
    dek** `tag:ci` anahtarı basabilir (kapsam yalnız `auth_keys`). Okuyucu kümesi değişmedi; şüphede
    client'ı Tailscale konsolunda iptal et ve yenisini (a)'daki gibi üret.
    (c) **Ephemeral olup olmadığı runner'dan ölçülemiyor** — ✅ **OPS-2:** artık **istenmiş** bir
    özellik (OAuth yolu `ephemeral=true` ile anahtar basar) ve cihaz `false` bildirirse kapı
    **kırmızı**; 1.94.2 bildirmediği için bugün uyarıda kalır. T134'ün ilk yeşil koşusu auth key'le
    cihazı **bıraktı** (`--state=mem:` ephemeral yapmadı); OAuth'la **kalmadı** — ilk OPS-2 deploy'u
    (koşu 39323391063) tailnet'te cihaz bırakmadı (*"OPS-2 kapanış ölçüsü"* → *"Ölçüldü"* (7)). Eski metin: tailscale v1.94.2'nin
    `status --json` çıktısında böyle bir alan yok (`ipnstate.PeerStatus`); kapı bunu
    **uyarı** olarak söyler. Ephemeral değilse her deploy Machines'te bir `github-*` cihazı
    bırakabilir — action'ın post adımı `tailscale logout` koşar (düğüm anahtarını geçersiz
    kılar), ama ephemeral olmayan bir cihazın kaydının konsolda kalıp kalmadığı **ölçülmedi**.
    Action `tailscaled`'i `--state=mem:` ile başlatır; bu, cihazı anahtardan **bağımsız**
    ephemeral yapıyor olabilir — **doğrulanamadı**. Ölçü bu yüzden konsoldur: **ilk deploy
    sonrası admin konsolunda ölçülür** (operatör adımı 5 (d); backlog T134'ün kapanış ölçüsü).
    (d) **~~Anahtar süreli (≤ 90 gün) ve~~ token süreli.** ✅ OPS-2: OAuth client'ın takvim süresi
    yok, auth key'in 90 günü kalktı. `github-deployer` token'ı **2027-10-19**'a kadar; dolunca deploy
    ilk küme çağrısında `401` ile **kümeye dokunmadan** düşer — zarar değil, kesinti. Yenileme tarihi
    backlog T135'te; değerler hiçbir yere yazılmaz.
    (e) **`tag:ci`'nin ACL'deki genişliği bu depodan görünmez.** ACL tailnet'te yaşar
    (kullanıcı 2026-10-17'de `tag:ci` → `10.43.0.1:443` yazdı); daha geniş bir kural
    eklenirse runner o kadarına ulaşır ve kapı bunu **görmez** — yalnız `10.43.0.1:443`'ün
    açık olduğunu ölçer.
    (f) **~~`TS_AUTHKEY` ve `KUBE_CONFIG` depo düzeyinde secret.~~** ✅ OPS-2: deploy'un beş
    secret'ı yalnız `main`'e açık `production` ortamında ve `deploy.yml` `environment: production`
    taşır (operatör adımı 5 (c)). ✅ Depo düzeyindeki eski secret'lar ilk OPS-2 deploy'undan önce
    silindi (2026-10-19, altı secret; depo düzeyinde secret yok) — madde kapandı. Ortamın
    dal politikası depo ayarlarında yaşar, bu depodan **görünmez** ve test edilemez.
