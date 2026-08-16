# Tappa — deployment (M8-02, packaging + cluster manifests)

Bu dizin **paketleme ve küme manifestlerini** taşır. M8-02'nin ikinci yarısı
(deploy/rollback/olay müdahalesi runbook'u, KEK döndürme aracı, DPA) burada
**yoktur**; bu dosya yalnızca *bu dosyaların nasıl kullanılacağını* ve
**kabul edilmiş sınırları** yazar.

Hedef: `https://tappa.everva.com.tr` · küme: k3s v1.35.4, tek node
(`k8s-1.fsn1.private`, Hetzner fsn1, `144.76.158.60`).

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
| `k8s/50-backup.yaml` | 🔴 **gecelik yedek CronJob'ı** (02:30 Malta) | **operatör** (bir kez; deploy'un `batch/cronjobs` yetkisi **yok** ve bilerek yok) |
| `k8s/postgres-init/02-app-password.sh` | `tappa_app`'e **girişi açan** üretim script'i | ConfigMap içinde |
| `../scripts/pg-backup.sh` | dump **ve içeriğinin doğrulanması** (satır sayısı, tablo kümesi, RLS/GRANT) | ConfigMap içinde (`deploy.yml` `--from-file`) |
| `../scripts/pg-backup-ship.sh` | dump'ı **node dışına** taşır + saklama süresini uygular | ConfigMap içinde (`deploy.yml` `--from-file`) |
| `../scripts/pg-restore-verify.sh` | 🔴 geri yüklenmiş bir veritabanının **kaynakla aynı** olduğunu ölçer | operatör, geri yüklemenin son adımı |
| `../scripts/verify-image.sh` | push öncesi imaj kapısı (izlenebilirlik/tzdata/goose/uid) | `deploy.yml` |

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

Yani boş kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır.
⚠️ Bu teorik değil: bu turda kümede kazara bırakılmış tam olarak bu durum bulundu ve
temizlendi (bozuk StatefulSet + `ImagePullBackOff` + Failed Job + bağlı 20Gi PVC).

**Elle deploy etmek gerekiyorsa** `.github/workflows/deploy.yml`'in adımlarını sırayla
izle: namespace → **`01-rbac.yaml`** (bir kez, cluster-admin ile) → `tappa-secrets`
(elle) → ConfigMap'ler (`--from-file` dahil) → Postgres → NetworkPolicy → migration
Job (**bekle**) → Deployment → Ingress. Tek tek `apply` etmek güvenlidir; **dizini
toptan** `apply` etmek değildir.

---

## Operatörün yapması gerekenler (sırayla, bir kez)

**1. DNS — ve bu adım en kritik olanı.**

`tappa.everva.com.tr` için **A kaydı → `144.76.158.60`**, Cloudflare proxy
**KAPALI** (gri bulut / "DNS only").

> ⚠️ `everva.com.tr` Cloudflare'de ve **proxy'li bir joker kayıt HÂLÂ var** —
> yeniden ölçüldü (2026-08-16): uydurma bir alt alan **hâlâ**
> `172.67.181.173`/`104.21.72.109`'a çözülüyor, yani bu adım **yeni bir host için
> hâlâ geçerli ve hâlâ varsayılanı yanlış**.
> ✅ **Ama `tappa.everva.com.tr` ARTIK proxy'li değil ve buradaki şimdiki zaman
> düzeltildi:** `dig +short tappa.everva.com.tr` → **`144.76.158.60`**,
> `scripts/verify-deployment.sh cloudflare tappa.everva.com.tr` → **exit 0,
> *"not proxied"***. Eskiden bu satır *"bugün zaten Cloudflare üzerinden cevap
> veriyor"* diyordu; o cümle 2026-08-15'te doğruydu, bugün değil.
> Proxy açık kalırsa uygulama her istemciyi bir Cloudflare adresi olarak görür:
> §5'in IP kanıtı (100 güven puanının 50'si) **hiç kimse için** doğru olamaz ve
> panel giriş bütçesi tüm müşteriler için tek adrese çöker (backlog T30).
> Gerekçenin tamamı `k8s/40-ingress.yaml` başında.

Doğrulama (kayıt açıldıktan sonra):

```bash
dig +short tappa.everva.com.tr                     # 144.76.158.60 olmalı
curl -sS -o /dev/null -D - https://tappa.everva.com.tr/ | grep -i 'cf-ray\|^HTTP/'
# cf-ray varsa kayıt hâlâ proxy'li demektir; YOKSA yalnız `HTTP/2 200` satırı çıkar.
# (Desende `^HTTP/` var, `^server` yok: origin `server` başlığı GÖNDERMİYOR ve durum
#  satırında `HTTP` ile `/` arasında iki nokta yok — eski desen sağlıklı durumda
#  hiçbir şey basmayıp exit 1 veriyordu, yani "komut bozuk" gibi görünüyordu.)
```

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

```bash
# üretim (yalnız bu iki komut, çıktıyı bir yere yapıştırmadan):
openssl rand -base64 32   # TAPPA_SESSION_HMAC_KEY / TAPPA_TAG_KEK / TAPPA_INVITE_HMAC_KEY
openssl rand -hex 32      # POSTGRES_PASSWORD / TAPPA_APP_PASSWORD  (URI'ye giriyorlar)
```

`TAPPA_INVITE_HMAC_KEY`, `TAPPA_SESSION_HMAC_KEY`'den **farklı** üretilmeli —
`config.Load` eşitlerse açılışta reddeder.

**3. Deploy'un kendi kimliği — `k8s/01-rbac.yaml` (bir kez, cluster-admin ile).**

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml    # namespace önce; RBAC onun içine giriyor
kubectl apply -f deploy/k8s/01-rbac.yaml
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
`secrets` yetkisi **tek isim üzerinde tek fiil** (`get` / `tappa-secrets`).
**Ne KAPANMADI, sayılıyor:** deploy kimliği bir `Job` yaratabilir (migration'ın
kendisi budur) ve bir Job'ın pod şablonu **kendi namespace'indeki her Secret'a**
referans verebilir — bu referans RBAC istemez. Kum havuzunda uçtan uca ölçüldü:
bu kurallarla yaratılan bir Job, listeleyemediği bir Secret'ın değerini okuyup
loguna bastı. Yani daraltma **okumayı** değil, **yıkıcı yarıyı** kaldırıyor
(artık `tappa-secrets`'ı yaratamaz, ezemez, silemez). Ayrıntı: sınır
**[deploy kimliği namespace'in sırlarını okuyabilir]**.

**4. Docker Hub — iki depo PUBLIC, iki GitHub secret'ı.**

🔴 **Bu adım kullanıcının işi ve deploy onsuz koşmaz.** İmajlar GHCR'dan Docker
Hub'a taşındı; gerekçesi ölçüm: bu node'un kubelet'i
`imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages` ile koşuyor
(KEP-2535), yani **kimlikle** çekilmiş bir imaj, kayıtlı kimliği olmayan bir pod'a
**düğümde dururken bile** verilmiyor (kimlik yok → `401` · kullanılmamış kimlik →
`401` · `imagePullPolicy: Never` → `ErrImageNeverPull`). Ayırt eden kontrol:
**public** bir imaj + `Never` → **önbellekten açıldı, exit 0**. **Public depo bu
kusuru tanım gereği yok ediyor** — kaydedilecek bir kimlik yoktur.

1. Docker Hub → **Account Settings → Personal access tokens** → **Read & Write**
   yetkili bir token üret.
2. GitHub deposuna iki secret yaz:

```bash
gh secret set DOCKERHUB_USERNAME --repo atknatk/tappa     # Docker Hub kullanıcı adın
gh secret set DOCKERHUB_TOKEN    --repo atknatk/tappa     # yukarıdaki token
```

3. `atknatk/tappa` ve `atknatk/tappa-migrate` depoları **Public** olmalı. İki yol
   var ve **birincisi tercih edilir**: (a) ilk deploy'dan **önce** Docker Hub'da
   *Create repository* ile ikisini **Public** olarak aç — böylece hiç private
   olmazlar; (b) ilk push'tan **sonra** Repository → Settings → Visibility → Public.
   ⚠️ **Bugün iki depo da YOK** (ölçüldü 2026-08-16: `hub.docker.com/v2/repositories/
   atknatk/tappa/` → **404**, `…/tappa-migrate/` → **404**), yani ilk push ikisini de
   **private** yaratacaktır.

   🔴 **AMA BU BİR TALİMAT DEĞİL, BİR KAPI — ve fark önemli.** `deploy.yml`,
   push'tan hemen sonra ve kümeye **hiçbir şey uygulamadan önce**, iki imajı da
   **kimliksiz** çekmeyi deniyor (*"Gate — both images must be pullable with NO
   credential"*). Depo private kaldıysa adım orada kırmızı olur, mesajı bu maddeyi
   adıyla gösterir ve **küme hiç değişmez**. Böylece *"public yap"* sağlanmayan bir
   garanti olmaktan çıkıp **mekanizması olan** bir koşula dönüşüyor — bu kartın imza
   kusurunun tam tersi.
   Kapı anonimliği `DOCKER_CONFIG`'i **boş bir dizine** çevirerek elde ediyor
   (`docker logout` değil: logout geliştirici makinesinde bir *credential helper*
   üzerinden gider — bu dosya yazılırken ölçüldü, `credsStore: desktop` — yani
   runner'dakinden farklı bir mekanizmayı sınardı). Ayırt edici kontrol: boş
   `DOCKER_CONFIG` + public imaj → **rc 0**; içinde **uydurma** bir docker.io kimliği
   olan `DOCKER_CONFIG` + aynı imaj → **rc 1, `unauthorized`**. İkincisi olmasa
   birincinin gerçekten anonim olduğu **kanıtlanamazdı**.

⚠️ **İki secret yokken ne oluyordu, ölçüldü:** tanımsız bir GitHub secret'ı **boş
dizeye** dönüşür, hata vermez. `docker login` o boş değerle **rc=1** ve
`username is empty` / `password is empty` diyor — hangi GitHub secret'ının eksik
olduğunu, nereden alınacağını **söylemiyor**. Bu yüzden `deploy.yml`'in **birinci**
adımı artık ikisini de sayıyor ve eksikse **hiçbir şey inşa etmeden** duruyor.

⚠️ **Public depo, İKİ imajın da herkesçe indirilebilir olması demektir.** İkisi de
`scratch` üzerine kurulu ve içerikleri `Dockerfile`'ın son iki aşamasından birebir
okunuyor: **uygulama** = CA paketi + `/tappa`; **migration** = CA paketi + `/goose` +
`/migrations` (20 `.sql`, hepsi zaten public depoda). Sır taşımıyorlar ve `deploy.yml`
push'tan **önce** içeriği kapılıyor (`scripts/verify-image.sh`).
⚠️ Burada bir tur boyunca *"imaj `scratch` + iki dosya"* yazdı — o cümle **iki
artefaktın yalnız birini** anlatıyordu. Karar yine de bilinçli verilmeli: açığa çıkan
şey kod değil ama **etiket listesi** herkese açık ve etiketler `sha-<12hex>`, yani
üretimde hangi commit'in koştuğu dışarıdan okunabilir hâle geliyor — sınır
**[sevk edilen commit'in kimliği herkese açık]**. Anonim çekme oran limiti de sınır
listesinde sayılıyor.

**Elenen iki seçenek ve gerekçeleri:** **GHCR public** — istenmediği için değil,
**ulaşılamadığı** için: paket görünürlüğünün **API'si yok** (üç kez ölçüldü:
`gh api -X PATCH /user/packages/container/tappa -f visibility=public` → **404, böyle
bir uç nokta yok**), yalnız arayüzden yapılıyor ve kullanıcı iki kez denedi.
**AWS ECR** — private ECR aynı kimlik sorununu dönen bir IAM token'ıyla geri
getirirdi; ECR Public yalnız `us-east-1`'den push kabul ediyor ve bir AWS hesabı
istiyor.

**5. GitHub secret'ı — `KUBE_CONFIG`.**

```bash
# 🔴 ADIM 3'TEKİ ServiceAccount'un TOKEN'IYLA üret, cluster-admin kubeconfig'inle DEĞİL.
# ⚠️ İstenen süre apiserver'ın üst sınırıyla KIRPILIR ve kubectl bunu bir uyarıyla
#    söyler ("requested expiration of ..., but the token will expire at ...").
#    Kırpılmışsa süre dolmadan yenilemek gerekir — takvime yaz, yoksa deploy sessizce
#    401 almaya başlar.
kubectl -n tappa create token github-deployer --duration=8760h
# ...bu token'ı bir kubeconfig'e yaz (cluster/server/CA aynı kalır, user değişir), sonra:
# ⚠️ `base64 -w0 <dosya>` DEĞİL — bu makinede (BSD/macOS base64) ölçüldü:
#    `base64 -w0 dosya` -> "base64: invalid argument", rc=64 ve STDOUT BOŞ, yani
#    boru `gh secret set`'e hiçbir şey vermez ve KUBE_CONFIG BOŞ yazılır. GNU
#    coreutils'te aynı komut çalışıyor. Aşağıdaki biçim ikisinde de çalışıyor
#    (ölçüldü, aynı çıktı): -w yok, satır sonlarını `tr` siliyor, girdi stdin'den.
base64 < ~/.kube/tappa-deployer.config | tr -d '\n' | gh secret set KUBE_CONFIG --repo atknatk/tappa
```

> ⚠️ **Bugün bu secret'ın hangi kimliği taşıdığı BU DOSYADAN doğrulanamaz** ve öyle
> yazılıyor. Eğer içindeki kubeconfig cluster-admin ise `01-rbac.yaml`'daki daraltma
> **hiçbir şeyi sınırlamaz** — cluster-admin bir kimlik Role'ü zaten aşar. Sınır
> **[kubeconfig'in kimliği doğrulanmadı]**.

**6. İlk deploy.** `main`'e push → `ci` yeşil → `deploy` kendiliğinden koşar.
Elle: Actions → `deploy` → Run workflow.

**7. İlk deploy sonrası — üç doğrulama (hiçbiri isteğe bağlı değil).**

```bash
# (a) TAPPA_TRUSTED_PROXIES doğru mu: mobil veriyle bir tap at, satırı oku
#     source_ip TELEFONUN genel adresi olmalı; 10.42.0.1 ise liste dar.
# (PGPASSWORD konteynerin kendi ortamından okunur -- parola ekrana basılmaz.)
kubectl -n tappa exec statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
  psql -U tappa_owner -d tappa -Atc \
  "SELECT ip_match, source_ip FROM transactions ORDER BY created_at DESC LIMIT 1;"'

# (b) rol ayrımı gerçekten uygulanmış mı
kubectl -n tappa exec statefulset/tappa-postgres -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" \
  psql -U tappa_owner -d tappa -Atc \
  "SELECT rolname, rolcanlogin, rolsuper, rolbypassrls FROM pg_roles WHERE rolname LIKE '"'"'tappa%'"'"' ORDER BY 1;"'
# tappa_app|t|f|f  ·  tappa_owner|t|t|t  ·  tappa_resolver|f|f|t

# (c) TLS + HSTS
curl -sS -o /dev/null -D - https://tappa.everva.com.tr/healthz | \
  grep -i 'strict-transport\|^HTTP'
```

**8. `/signup`'tan ilk işletmeyi aç.** İlk deploy **boş şemadır**, seed yoktur
(kullanıcı kararı). Sonra `/admin/legal`'e gir, reddetme sayfasının bastığı kendi
`admin_users.id`'ni `05-config.yaml` → `TAPPA_OPERATOR_ADMIN_IDS`'e yaz ve
`kubectl -n tappa rollout restart deployment/tappa`.

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

```bash
kubectl get nodes                      # k8s-1.fsn1.private — tek node
kubectl get sc                         # local-path (default), rancher.io/local-path, Delete
kubectl get csidrivers                 # No resources found
kubectl get pv -o custom-columns=SC:.spec.storageClassName --no-headers | sort -u   # local-path
kubectl get cronjob -A                 # hiçbir namespace'te yedek işi yok — emsal de yok
```

`local-path`, o tek node'un diskinde bir dizindir. Oraya yazılan bir dump, kopyası
olduğu veritabanıyla **aynı olayda** ölür. Bu yüzden dump `emptyDir`'de hazırlanır
(pod'la birlikte silinir) ve **tek kalıcı artefakt node'un dışına gidendir**;
gönderemezse iş **kırmızı olur** ve geride *"yedek sanılacak"* bir kopya bırakmaz.

| Seçenek | Karar | Gerekçe |
|---|---|---|
| **AB bölgesinde S3 uyumlu nesne depolama** (ör. Hetzner Object Storage `fsn1`) | ✅ **önerilen** | Aynı veri merkezi ama **ayrı depolama sistemi ve ayrı arıza alanı**; §1'in AB şartına uyuyor; rclone'un `crypt` katmanıyla şifreleniyor |
| **SFTP hedefi** (Hetzner Storage Box vb.) | ✅ kabul edilebilir | Aynı rclone config'iyle; kimlik bir SSH anahtarı olur |
| **Kümedeki bir PVC** | ❌ **elendi** | `local-path` = aynı node'un diski. Sınır 1'in tarif ettiği arızada **hiçbir şey kurtarmaz** |
| **GitHub Actions'ın dump'ı çekmesi** | ❌ **elendi** | Çalışanların mesai kayıtları GitHub artefakt deposuna çıkardı — AB bölgesi dışına ve DPA'sız bir işleyene (Q23) |
| **Kümeye ikinci bir Postgres replikası** | ❌ **elendi** | Tek node'da replika **aynı diski** paylaşır; ayrıca §4.3 hatası (yanlış migration, kötü DELETE) replikaya **anında** kopyalanır — replika yedek değildir |

**(b) Sırrı yaz — iki anahtar, ve şifreleme anahtarı `TAPPA_TAG_KEK` ile AYNI YERDE
DURMAZ.**

```bash
# rclone config'ini KENDİ makinende üret (rclone config), sonra:
kubectl -n tappa create secret generic tappa-backup-target \
  --from-file=rclone.conf=$HOME/.config/rclone/rclone.conf \
  --from-literal=BACKUP_REMOTE='tappa-backup:<bucket>/tappa-prod'
```

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

```bash
# (1) kümedeki kopyanın parmak izi — değer ekrana BASILMAZ, yalnız sha256 öneki
kubectl -n tappa get secret tappa-backup-target -o jsonpath='{.data.rclone\.conf}' \
  | base64 -d | sha256sum | cut -c1-16
# (2) emanetteki kopyanın parmak izi (kasadan/parola yöneticisinden indirdiğin dosya)
sha256sum /path/to/escrowed-rclone.conf | cut -c1-16
# 🔴 İKİSİ EŞİT DEĞİLSE EMANET YOK DEMEKTİR. Eşitse bu adım tamamdır.
```

⚠️ Bu iki komut `tappa-backup-target`'ı okur, **`tappa-secrets`'ı değil** — bu dosya
`tappa-secrets` üzerinde `jsonpath` denemeyi §4.7 gereği yasaklıyor. Ve çıktı bir
**hash önekidir**, sırrın kendisi değil.

**(c) CronJob'ı uygula ve ilk koşuyu ELLE tetikle** (gece 02:30'u bekleme — çalışmayan
bir yedeği ertesi sabah öğrenmek, hiç yedek olmamasından farksızdır):

```bash
kubectl apply -f deploy/k8s/50-backup.yaml
kubectl -n tappa create job tappa-backup-first --from=cronjob/tappa-backup
kubectl -n tappa wait --for=condition=complete job/tappa-backup-first --timeout=3600s
kubectl -n tappa logs job/tappa-backup-first -c dump-and-verify
kubectl -n tappa logs job/tappa-backup-first --tail=20            # ship konteyneri
kubectl -n tappa delete job tappa-backup-first
```

`dump-and-verify` logunun son satırları şu şekli taşımalı (ölçülmüş gerçek çıktı,
2.1 milyon satırlık bir veritabanından):

```
pg-backup: connected as tappa_owner (rolsuper=true rolbypassrls=true), row_security stays off
pg-backup: live schema: 19 tables, 18 policies, 18 tables with FORCE RLS, goose 20
pg-backup: verified: 19/19 tables present, 2118896 rows in the dump (2118896 live now), 18 policies, 18 ENABLE / 18 FORCE RLS, 105 GRANTs
pg-backup: backup instant (restore rewinds to here): 2026-08-16T17:04:47Z
```

⚠️ **Taze bir kurulumda satır sayısı `0` olur ve bu DOĞRUDUR** — hiçbir kontrol
*"sıfırdan fazla satır"* istemiyor, çünkü sağlıklı bir yeni kurulum **boştur**. Kırmızı
olan şey *"canlıda satır var, dump'ta yok"*tur.

---

## Elle deploy / rollback

> 🔴 **ÖNCE ŞUNU ÖLÇ: BU BÖLÜM DOCKER HUB'I TARİF EDİYOR, KÜME BUGÜN HÂLÂ GHCR
> KOŞUYOR.** Aşağıdaki `set image` komutu, **kümenin bugünkü hâlinde koşulursa
> ÜRÜNÜ DÜŞÜRÜR**: `docker.io/atknatk/tappa` deposu **yok** (ölçüldü 2026-08-16:
> `hub.docker.com/v2/repositories/atknatk/tappa/` → **404**), yani `1/1 Running` bir
> pod'un yerine **çekilemeyen** bir imaj konur → `ImagePullBackOff` → 04:00 vardiyası
> tap sayfasını **hiç** yükleyemez.
>
> **Ön koşul — bu üçü doğru olana kadar aşağıdaki `docker.io/...` komutunu KOŞMA:**
> operatör adımı 4 tamamlanmış (iki GitHub secret'ı + iki depo **Public**) **ve**
> ondan sonra en az bir `deploy` koşusu yeşil olmuş olmalı. Ölç, iddia etme:
> ```bash
> kubectl -n tappa get deployment tappa \
>   -o jsonpath='{.spec.template.spec.containers[0].image}{"  ps="}{.spec.template.spec.imagePullSecrets[*].name}{"\n"}'
> # ghcr.io/... + ps=ghcr   -> küme HÂLÂ GHCR'da: aşağıdaki komutta etiketi ghcr.io/... yaz
> # docker.io/... + ps yok  -> geçiş inmiş: aşağıdaki komut olduğu gibi doğru
> ```
> **Bugünkü gerçek ölçüm (2026-08-16, salt okuma):** `deployment/tappa` →
> `ghcr.io/atknatk/tappa:sha-353897c6d5f6`, `imagePullSecrets: ghcr`; üç ReplicaSet'in
> **üçü de** `ghcr.io/...` + `ps=ghcr`; `secret/ghcr` **var** (18 sa). Yani geçiş
> **henüz inmedi**. Bu, bölüm 1'in *"Ağaçtaki manifestler taşıyor; KÜMEDEKİ nesneler
> henüz taşımıyor"* uyarısının aynısıdır — orada yazılıydı, burada yazılı değildi.

> 🔴 **AŞAĞIDAKİ BLOĞU TOPTAN KOPYALAMA — ÜÇÜNCÜ KOMUT AYRI BİR KARAR.** İlk komut
> yer tutucu taşıdığı için kopyalayanı **durdurur**, `rollout undo` **durdurmaz**;
> onu koşmadan önce bloğun **altındaki** uyarıyı oku. (Bu üçlü bilerek tek blokta
> duruyor, çünkü sırayla okunmalı — ama `undo` bir kurtarma değil, koşullu bir
> harekettir.)

```bash
# ⚠️ ETİKETİN KAYNAĞINI YUKARIDAKİ ÖLÇÜMDEN AL: küme GHCR'daysa ghcr.io/atknatk/tappa,
#    Docker Hub geçişi indiyse docker.io/atknatk/tappa.
# 🔴 YER TUTUCU SESSİZCE KABUL EDİLİR — ölçüldü (çevrimdışı, --local):
#    `kubectl set image ... tappa='<registry>/atknatk/tappa:sha-<12hex>'` çıktısı
#    BİREBİR `<registry>/atknatk/tappa:sha-<12hex>`; istemci tarafında imaj
#    referansının doğrulaması YOKTUR. Yani yanlış yazılmış bir etiket buradan geçer
#    ve arızayı ancak kubelet bildirir (`ImagePullBackOff`).
kubectl -n tappa set image deployment/tappa tappa=<registry>/atknatk/tappa:sha-<12hex>
kubectl -n tappa rollout status deployment/tappa

# 🔴 ÜÇÜNCÜ KOMUTU KOŞMADAN ÖNCE ALTTAKİ UYARIYI OKU.
kubectl -n tappa rollout undo deployment/tappa      # bir önceki imaja
```

> 🔴 **`rollout undo` HÂLÂ RİSKLİ — ÇÜNKÜ KÜME HÂLÂ GHCR'DA.** Burada bir tur boyunca
> *"o uyarı artık geçersiz"* yazdı; **ağaç için** doğruydu, **küme için değil**, ve bu
> tam olarak bu kartın imza kusurudur — sağlanmayan bir garantiyi ilan etmek.
> Bugünkü ölçüm: üç ReplicaSet'in **üçü de** `ghcr.io/...` + `imagePullSecrets: ghcr`,
> yani `rollout undo` bugün **tam olarak** kaldırdığım uyarının tarif ettiği duruma
> düşer: kubelet kimlik doğruluyor (KEP-2535), eski imaja dönmek yeniden çekme
> tetikliyor, `secret/ghcr`'ın token'ı ise sonraki deploy'da ezilmiş → **401 →
> `ImagePullBackOff`**, tam da olay anında.
> ✅ **Uyarı yalnız şu koşulda düşer:** Docker Hub geçişi kümeye inip **hem
> Deployment hem de dönmek istediğin ReplicaSet** `docker.io/...` gösterdiğinde —
> public depoda kaydedilecek kimlik yoktur (ölçülmüş kontrol: public imaj +
> `imagePullPolicy: Never` → **düğüm önbelleğinden açıldı, exit 0**). O gün gelene
> kadar bu satır **geçerlidir**.
>
> ⚠️ **Her hâlükârda rollout'u izle** — geri alma başka sebeplerle de takılabilir
> (düğüm imajı düşürmüştür ve Docker Hub'ın anonim bütçesi tükenmiştir: sınır
> **[Docker Hub anonim çekme bütçesi]**):
> ```bash
> kubectl -n tappa rollout undo deployment/tappa
> kubectl -n tappa rollout status deployment/tappa --timeout=120s || \
>   kubectl -n tappa get pod -o wide
> ```

> 🔴 **Rollback şemayı geri almaz.** `goose down` bu iş akışında **yoktur** ve
> bilinçlidir: geri alınabilir olmak (`-- +goose Down` dolu) ile *otomatik olarak
> geri alınmak* aynı şey değildir. Bir migration'ı geri almak, `transactions`
> immutable olduğu için (§4.3) veri kaybı anlamına gelebilir — el ile, ölçerek
> yapılır.

---

## Yedek ve geri yükleme

> 🔴 **ÖNCE ŞUNU ÖLÇ: BU BÖLÜM AĞACI TARİF EDİYOR; KÜMEDE CronJob OLUP OLMADIĞI AYRI
> BİR SORU.** Ağaç ile küme bu dosyada daha önce üç kez ayrıştı ve üçünde de ayrımı
> yazmayan cümle yanlış çıktı. Teşhise **kümeye sorarak** başla:
> ```bash
> kubectl -n tappa get cronjob tappa-backup                 # yoksa: operatör adımı 9 yapılmamış
> kubectl -n tappa get job -l app.kubernetes.io/component=backup   # son geceler
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

Her koşu iki dosya bırakır: `tappa-<UTC damgası>.sql.gz` ve yanında bir
`.manifest`. Manifest'te sır yoktur; içinde sha256, yedek anı, sunucu sürümü,
`goose` sürümü, tablo/politika/GRANT sayıları ve **tablo tablo satır sayıları** var.

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

### Son yedek ne durumda — iddia değil, komut

```bash
kubectl -n tappa get job -l app.kubernetes.io/component=backup \
  -o custom-columns=NAME:.metadata.name,DONE:.status.succeeded,FAIL:.status.failed,START:.status.startTime
kubectl -n tappa logs job/<job> -c dump-and-verify | tail -5
```

Hedefteki kopyayı **kendi makinenden** (kümeye hiç dokunmadan) listelemek:

```bash
rclone lsl tappa-backup:<bucket>/tappa-prod          # aynı rclone.conf ile
```

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
curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/healthz
curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/readyz
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

**3. Yedeği indir ve bütünlüğünü doğrula.**

```bash
STAMP=<damga>                       # indirdiğin dizinin adı, ör. 20260816T023000Z
rclone copy "tappa-backup:<bucket>/tappa-prod/$STAMP" ./restore/

M=./restore/tappa-$STAMP.manifest
G=./restore/tappa-$STAMP.sql.gz
want=$(awk '/^sha256 /{print $2}' "$M")
have=$(sha256sum "$G" | awk '{print $1}')     # macOS'ta yoksa: shasum -a 256 "$G"
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

gzip -dc ./restore/tappa-<damga>.sql.gz | kubectl -n tappa exec -i statefulset/tappa-postgres -- \
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
> ve **0 `CREATE ROLE`**. Roller küme kapsamlı nesnelerdir; `tappa_app` ve
> `tappa_resolver` `scripts/db-init/01-roles.sql`'den, `tappa_owner` `POSTGRES_USER`'dan
> gelir. Aynı instance'a geri yüklerken zaten oradalar; taze bir pod'da init script'i
> onları yaratıyor. **Bu yüzden geri yükleme yolu bu repodan başlar, dump'tan değil.**
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
STAMP=$(awk '/^started_at/{print $2}' ./restore/*.manifest)
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
  > lost-window.csv <<SQL
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
> `psql` hatalı bir ifadede **exit 0** verir, bayrakla **exit 3**. `> lost-window.csv`
> biçiminde bayraksız bir `COPY` hatası **boş bir CSV + exit 0** üretirdi ve operatör
> bunu *"kayıp yok"* diye okuyup adım 8'i (yeniden giriş) **atlardı**.

🔴 **Bu CSV yine de çalışanların mesai kaydıdır**: şifreli bir yerde tut, işi bitince
sil, ve kimseye e-postayla gönderme (§4.7 / GDPR). Yeniden girişleri `channel='manual'` +
`entered_by` ile yapılır ve raporlarda **ayrı görünür** (§5) — bu bir kusur değil,
kaydın gerçekte nasıl doğduğunun dürüst hâlidir.

**6. Geri yüklenen kopyayı DOĞRULA — takastan önce.**

```bash
PGHOST=<postgres> PGDATABASE=tappa_restore PGUSER=tappa_owner \
PGPASSWORD=<owner> TAPPA_APP_PASSWORD=<app> \
  scripts/pg-restore-verify.sh ./restore/tappa-<damga>.sql.gz ./restore/tappa-<damga>.manifest
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

```bash
kubectl -n tappa scale deployment/tappa --replicas=0     # bağlantıları kes
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d postgres' <<SQL
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname IN ('tappa','tappa_restore') AND pid <> pg_backend_pid();
ALTER DATABASE tappa RENAME TO tappa_damaged_$(date -u +%Y%m%d);
ALTER DATABASE tappa_restore RENAME TO tappa;
SQL
# 🔴 VE VARSAYILANI YENIDEN DARALT — dump onu az önce genişletti (aşağıdaki not).
kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -U tappa_owner -d tappa' <<'SQL'
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE UPDATE, DELETE ON TABLES FROM tappa_app;
SQL

kubectl -n tappa scale deployment/tappa --replicas=1
kubectl -n tappa rollout status deployment/tappa
curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/readyz     # 200 bekleniyor
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

**8. Kayıp aralığını yeniden gir.** 5. adımın CSV'sindeki her kayıt panelden manuel
kayıt olarak girilir (`channel='manual'`, `entered_by` dolu). Otomatik bir toplu
`INSERT` **bilerek yazılmadı**: bu satırlar hukuki kayıt ve kimin girdiği
görünmelidir.

---

#### 🔴 B YOLU — INSTANCE YOK (node gitti, PVC gitti): taze bir pod'a geri yükleme

🔴 **A YOLU'nun hiçbir adımı burada uygulanamaz** ve en önemlisi: **kayıp aralığını
ölçecek bir kaynak yok.** Kayıp, tanımı gereği *"yedek anından arızaya kadar olan her
şey"*tir ve bunu **ancak yedeğin `started_at`'i ile arızanın saati** arasından
tahmin edebilirsin. Bunu bir kayıp beyanı olarak yaz; bir kaza olarak değil.

**B1. Postgres'i normal yoldan ayağa kaldır — ve init script'leri KOŞSUN.**
Roller (`tappa_app`, `tappa_resolver`) dump'ta **yoktur**, bu repodan gelir.

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/01-rbac.yaml          # cluster-admin ile, bir kez
# tappa-secrets'ı emanetten geri yükle (operatör adımı 2)
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

```bash
rclone copy tappa-backup:<bucket>/tappa-prod/<damga> ./restore/
gzip -dc ./restore/tappa-<damga>.sql.gz | kubectl -n tappa exec -i statefulset/tappa-postgres -- \
  sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -U tappa_owner -d tappa'

# 🔴 B2'NIN AYNASI — dump varsayılanı yeniden genişletti, geri daralt.
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

```bash
PGHOST=<postgres> PGDATABASE=tappa PGUSER=tappa_owner \
PGPASSWORD=<owner> TAPPA_APP_PASSWORD=<app> \
  scripts/pg-restore-verify.sh ./restore/tappa-<damga>.sql.gz ./restore/tappa-<damga>.manifest
```

**Çıkmazsa `exit 0`, uygulamayı açma.** Araç 45 tablo + 454 sütun yetkisini dump'ın
ilan ettiğiyle karşılaştırıyor ve `42501`'i assert ediyor; B2 atlanmışsa **fazlaları
adıyla** listeler.

**B5. Uygulamayı aç ve kayıp aralığını İLAN ET.**

```bash
kubectl apply -f deploy/k8s/05-config.yaml
kubectl apply -f deploy/k8s/12-networkpolicy.yaml
kubectl apply -f deploy/k8s/20-app.yaml
kubectl apply -f deploy/k8s/40-ingress.yaml
kubectl -n tappa rollout status deployment/tappa
curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/readyz
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

---

## Olay müdahalesi — belirti → sebep

> **Bu bölümdeki satırların ÇOĞU bu kümede fiilen ölçüldü** — ve ölçülmemiş olanlar
> **satırında işaretlidir**. Uydurma senaryo yok; bir belirti burada yoksa, o belirti
> **bu repoda henüz görülmedi** demektir ve teşhis ölçümle başlamalıdır, bu listeyi
> zorlayarak değil.
>
> 🔴 **BU CÜMLE BİR TUR BOYUNCA *"her satır fiilen ölçüldü"* DİYORDU VE ALTINA
> ÖLÇÜLMEMİŞ SATIRLAR EKLENDİ** (bölüm 2'nin Docker Hub satırları: depolar bu kümede
> hiç var olmadı, oran limiti yalnız **başlık** olarak ölçüldü). Değişmemiş bir
> garanti cümlesinin altına yeni satır eklemek, garantiyi sessizce yalanlar —
> bu dosyanın imza kusurunun bir başka yüzü.
>
> 🔴 **VE BÜTÜN BÖLÜM İÇİN GEÇERLİ TEK ÖN KOŞUL: AĞAÇ İLE KÜME AYNI ŞEY DEĞİL.**
> Ağaç Docker Hub'ı ve `imagePullSecrets`'sız pod'ları tarif ediyor; **küme bugün
> hâlâ GHCR koşuyor** (ölçüldü 2026-08-16: `deployment/tappa` →
> `ghcr.io/atknatk/tappa:sha-353897c6d5f6` + `imagePullSecrets: ghcr`, üç
> ReplicaSet'in üçü de aynı, `secret/ghcr` **var**). Teşhise başlamadan **kümeye
> sor**, ağaca değil:
> ```bash
> kubectl -n tappa get deployment tappa \
>   -o jsonpath='{.spec.template.spec.containers[0].image}{"  ps="}{.spec.template.spec.imagePullSecrets[*].name}{"\n"}'
> kubectl -n tappa get secret ghcr --ignore-not-found -o name   # boşsa geçiş inmiştir
> ```
>
> **Önce ölç, sonra dokun.** Kesintinin var olup olmadığının tek dış kanıtı:
> ```bash
> curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/healthz  # süreç ayakta mı
> curl -sS -o /dev/null -w '%{http_code}\n' https://tappa.everva.com.tr/readyz   # DB'ye ulaşıyor mu
> ```
> `/healthz` hiçbir şeye dokunmaz, `/readyz` havuzdan bağlantı alır. **200/200** =
> ürün çalışıyor; **200/503** = süreç ayakta ama veritabanı yok; **ikisi de yok** =
> pod ya da ingress.
>
> 🔴 **ÜÇÜNCÜ BİR SORU VAR — *"ayağa KALKABİLİR miyim"* — VE BU KÜMEDE HÂLÂ
> GEÇERLİ.** Kaynağı `secret/ghcr`: çalışan pod'un imajı **kayıtlı** bir kimlikle
> çekilmiştir ve sonraki bir deploy o Secret'ı ezdiğinde pod **yeniden başlayamaz**
> hâle gelir, üstelik hiçbir belirti vermeden.
> ⚠️ **Burada bir tur boyunca *"o soru ARTIK YOK, bu durum oluşamıyor"* yazdı. Ağaç
> için doğruydu, KÜME için değil** — ölçüldü 2026-08-16: `secret/ghcr` **var** (18 sa),
> Deployment ve üç ReplicaSet'in üçü de `imagePullSecrets: ghcr`. Soru ancak Docker
> Hub geçişi kümeye indiğinde düşer (public depoda kaydedilecek kimlik yoktur).
> ⚠️ **Ve onu ölçen komut (`scripts/verify-deployment.sh pull-credential`)
> KALDIRILDI** — bugün elde olan tek kanıt elle okumaktır:
> ```bash
> kubectl -n tappa get secret ghcr -o jsonpath='{range .metadata.managedFields[*]}{.time}{"  by "}{.manager}{"\n"}{end}'
> kubectl -n tappa get pod -l app.kubernetes.io/component=server \
>   -o jsonpath='{range .items[*]}{.status.startTime}{"  "}{.metadata.name}{"\n"}{end}'
> # En yeni secret/ghcr yazımından ÖNCE başlamış bir pod yeniden başlayamaz.
> # Çare: yeni ve BAŞARILI bir deploy koşusu. (Komut hüküm vermez, kanıt basar.)
> ```
> Silinme gerekçesi kayıtta: mod, geçiş indikten sonra konusu var olmayan bir Secret
> olacağı için her sağlıklı kümede *"UNREADABLE"* diyecekti. **Geçiş inene kadar
> yukarıdaki iki satır onun yerini tutar.** Ders defterde: kart düzeltmesi, FAZ C 6. tur.

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
> # "the NAME RESOLVES to: …"        → soket/politika: bu bölümü oku
> # "the NAME … DOES NOT RESOLVE"    → DNS/Service: bu bölüm SENİN SORUNUN DEĞİL, 5'e geç
> #
> # ⚠️ `container wait-for-postgres is not valid for pod … out of: goose` alıyorsan
> # o pod bu initContainer'dan ÖNCEKİ bir manifestle doğmuştur — yani FAZ C henüz
> # kümeye inmemiştir. Bu bir arıza değil; aşağıdaki ölçüm komutuyla doğrula.
> ```
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
>
> # ⚠️ `--rm` pod'u YALNIZ kubectl bağlı kalırsa siler. Bu komut tam da pod'ların
> # takıldığı bir olayda koşulur, yani Ctrl-C ihtimali yüksek. Her hâlükârda:
> kubectl -n tappa delete pod dnscheck --ignore-not-found
> ```
> ⚠️ **BU SONDA `postgres:17-alpine` İSTİYOR, yani sınır
> **[Docker Hub anonim çekme bütçesi]**'nden bir çekme daha harcayabilir** — ve tam olarak `429` gördüğün bir
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
```bash
kubectl -n tappa get netpol                      # kural duruyor mu
kubectl -n tappa get pod <pod> -o jsonpath='{.metadata.labels}'   # etiket doğru mu
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

### 2. `ImagePullBackOff` — dört sebep, ve hangisinin geçerli olduğu KÜMEYE bağlı

> 🔴 **ÖNCE HANGİ DÜNYADASIN: bu bölümün ilk üç satırı DOCKER HUB dünyasına,
> dördüncüsü bugünkü GHCR kümesine ait.** Ağaç geçişi tarif ediyor, küme henüz
> inmedi (yukarıdaki önsözdeki iki komutla ölç). ⚠️ Burada bir tur boyunca başlık
> *"artık kimlik sorunu DEĞİL, iki başka sebep"* diyordu ve **iki şeyi birden**
> yanlış söylüyordu: tablo o an **üç** satır taşıyordu (bölümün kendi metni de
> *"üç satırda da"* diyordu; bugün **dört** satır ve metin de dört diyor), ve kimlik
> sorunu **bu kümede hâlâ canlı** — `secret/ghcr` **var**
> (ölçüldü 2026-08-16, 18 sa), Deployment ve üç ReplicaSet'in üçü de
> `imagePullSecrets: ghcr`. Yani *"`secret/ghcr` arayan biri var olmayan bir nesneyi
> arar"* cümlesi de **yanlıştı** ve kaldırıldı.

**Belirti.** `Failed to pull image ...`, `ErrImagePull`, `ImagePullBackOff`.

**Sebep — `kubectl -n tappa describe pod <pod>` mesajı hangisini söylüyor:**

| mesaj | sebep | çare — **satır bazında; genel bir çare YOK** |
|---|---|---|
| `pull access denied` / `repository does not exist` | Docker Hub deposu **private** | **Docker Hub → Repository → Settings → Public** (operatör adımı 4, madde 3). **Sonra** yeni bir deploy koşusu. 🔴 **Sırayı ters çevirme:** depo private iken yeni koşu `deploy.yml`'in *"Gate — both images must be pullable with NO credential"* adımında (push'tan sonra, `kubectl` daha kurulmadan) **tanımı gereği** durur ve *"Nothing has been applied to the cluster"* der — yani pod açılmamaya **devam eder** |
| `toomanyrequests` / `429` ⚠️ *bu kümede hiç görülmedi; yalnız başlık ölçüldü* | anonim çekme bütçesi doldu — sınır **[Docker Hub anonim çekme bütçesi]** (ölçüldü: `x-ratelimit-limit: 100;w=3600`, IP başına) | **Pencerenin dolmasını bekle** — Deployment pod'u kendiliğinden toparlar. Yeni deploy koşusu bunu **düzeltmez**: kapı runner'ın adresinden geçse bile çeken taraf **düğüm**dür. 🔴 **Ama migration `Job`'ı beklemeyle KURTULMAZ:** `activeDeadlineSeconds: 600` + `backoffLimit: 2`, yani saatlik pencereden **çok önce** kalıcı `Failed` olur → Job için **yine de yeni bir koşu** gerekir (pencere dolduktan sonra). Çalışan bir pod etkilenmez (imaj düğümde) |
| `manifest unknown` / `not found` | etiket yok — `:deploy-placeholder` kalmış (dizini toptan `apply` etmişsin) ya da push başarısız olmuş | **Yeni bir deploy koşusu** — doğru etiketi o yazar. `deploy.yml`'in `Push` adımını da oku; manifestleri elle `apply` etme |
| `401 Unauthorized` **(BUGÜNKÜ KÜME)** | `secret/ghcr`'ın token'ı ölü — kubelet kimlik doğruluyor (KEP-2535) ve pod'un imajını çeken token sonraki deploy'da ezilmiş. **Bu satır bu kümede 2026-08-15'te fiilen ölçüldü: 4 dk 39 sn** | **Yeni ve BAŞARILI bir deploy koşusu** (sırrı tazeler). Koşamıyorsan takılmış pod'u yeniden yarat — aşağıdaki uyarıyı okuyarak. ⚠️ Bu satır Docker Hub geçişi kümeye indiğinde **düşer**; o güne kadar geçerlidir |

> 🔴 **BURADA BİR TUR BOYUNCA *"Çare (her üçünde de) yeni bir deploy koşusudur"*
> YAZDI VE İLK İKİ SATIRDA **GARANTİLİ BAŞARISIZDI** — güvenlik denetimi bunu §4.6'nın
> altyapı yüzü olarak bloklayan saydı, haklı olarak: bu bölümün müdahale ettiği hâl
> uygulama pod'unun **hiç açılmadığı** hâldir, 04:00 vardiyası tap sayfasını
> yükleyemez, ve **kaydedilemeyen dokunuş kaybolan kayıttır**. Bir runbook'un
> operatörü tek ve **kesin işe yaramayacak** bir eyleme yönlendirmesi, hiçbir şey
> yazmamaktan kötüdür. Doğru cevap **satır bazındadır** ve zaten tablonun kendi
> sütununda duruyordu; genel cümle kaldırıldı.

> 🔴 **VE BİRİNCİ SATIRIN YANINDA *"bunu kümede görüyorsan iş akışının kapısı
> atlanmıştır, yani buraya ancak elle `apply` edilmiş bir manifestle gelinir"*
> YAZIYORDU — BU DA YANLIŞTI, ve bu dosya kendini sınır
> **[Docker Hub anonim çekme bütçesi]** maddesinde zaten yalanlıyordu.** Elle hiçbir `apply` olmadan üretilebilir,
> dört adımda ve **hepsi o maddede sayılı**: (1) deploy N yeşil — depolar public,
> kapı geçti, pod çalışıyor; (2) depo **sonradan** private'a çevrilir — kapı yalnız
> **deploy anını** ölçer ve bunu yapan kimseyi engelleyemez; (3) düğüm imajı düşürür
> — kubelet imaj GC'si (bu node'da `imageMinimumGCAge: 2m0s`), `crictl rmi`, ya da
> düğüm yeniden kurulumu; (4) pod yeniden başlar → `pull access denied`.
> **Doğru cümle:** kapı bir **deploy anı** ölçümüdür, süregelen bir garanti değil;
> bu satırı elle apply olmadan da görebilirsin ve o zaman aranacak şey *"kim elle
> apply etti"* değil, **deponun bugünkü görünürlüğüdür**. Yanlış cümle operatörü
> hiç olmamış bir olayı ararken pod'u kapalı bırakırdı.

**Dört satırda da ortak olan tek şey:** takılmış pod'u yeniden yaratmak kubelet'in
büyüyen geri çekilme aralığını sıfırlar — yani **satırın kendi çaresi uygulandıktan
sonra** pod'un dakikalarca beklemesini engeller.

🔴 **AMA HANGİ POD OLDUĞUNA BAK — üç tipin üçü de farklı sonuç veriyor, ve
*"SADECE `ImagePullBackOff` olanı"* bunları AYIRMIYOR:**

| pod | `delete` sonucu |
|---|---|
| `tappa-<hash>-<hash>` (Deployment) | **zararsız** — ReplicaSet yerine yenisini koyar, `maxUnavailable: 0` ile diğer replika servis verir |
| `tappa-migrate-<hash>` (**Job**) | 🔴 **`.status.failed`'ı ARTIRIR.** `backoffLimit: 2` olduğu için **üç silme Job'ı kalıcı `Failed`** yapar; sonra yalnız yeni bir deploy koşusu (Job'ı silip yeniden yaratır) kurtarır |
| `tappa-postgres-0` (**StatefulSet**) | 🔴 **VERİTABANINI YENİDEN BAŞLATIR.** Açık bağlantılar kopar, uygulama `/readyz` 503 verir. Bunu yalnız Postgres'in kendi imajı çekilemiyorsa yap |

```bash
kubectl -n tappa get pod -o wide            # ÖNCE hangi tip olduğuna bak (ada göre)
kubectl -n tappa delete pod <stuck-pod>     # SADECE ImagePullBackOff/ErrImagePull olanı
```
⚠️ `deploy.yml`'de bunu otomatik yapan bir adım **vardı ve kaldırıldı** — dayandığı
öncül (bayat çekme kimliği) **ağaçta** artık yok, ve premisi yanlış olan yıkıcı bir
adımı tutmak bu kartın imza kusurudur. ⚠️ **Kümede o öncül hâlâ geçerli** (yukarıdaki
dördüncü satır), yani geçiş inene kadar bu işi **elle** ve yukarıdaki tabloya bakarak
yapmak gerekiyor.

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
```bash
kubectl -n tappa logs job/tappa-migrate -c wait-for-postgres   # ulaşabildi mi
kubectl -n tappa logs job/tappa-migrate -c goose               # DDL ne dedi
# ⚠️ Birincisi `container wait-for-postgres is not valid for pod … out of: goose`
# derse o Job initContainer'sız bir manifestten doğmuştur (FAZ C kümeye inmemiş);
# yalnız `-c goose` çalışır. Bölüm 1'deki ölçüm komutu hangisi olduğunu söyler.
```
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
```bash
kubectl -n tappa get svc tappa-postgres                 # Service var mı
kubectl -n tappa get endpointslice                      # AYRI çağrı: uç noktalar
# ⚠️ İkisini tek `get`'e koyma: `get svc tappa-postgres endpointslice` her iki adı da
# `svc` kaynağında arar ve `services "endpointslice" not found` + exit 1 verir —
# yani aradığın arızanın kendisi gibi görünen sahte bir hata.
kubectl -n tappa get pod tappa-postgres-0               # Ready mi (headless kaydın şartı)
kubectl -n kube-system get pod -l k8s-app=kube-dns      # CoreDNS ayakta mı
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

## Kabul edilmiş sınırlar — hepsi bilinçli, hiçbiri kaza

1. **[yedek kümede HENÜZ YOK]** — 🔴 **BU MADDE ESKİ *[yedek YOK]* MADDESİNİN
   YERİNDE DURUYOR, VE ANAHTARIN DEĞİŞME SEBEBİ ANAHTARIN ARTIK YALAN OLMASI.**
   Eski madde *"Yedek YOK, replika YOK"* diyordu ve **bugün ağaç için yanlış, küme
   için hâlâ doğru** — bu dosyada üç kez bedeli ödenmiş ayrım (bkz. sınır
   **[Docker Hub anonim çekme bütçesi]**), o yüzden ikisi ayrı yazılıyor.
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
3. **`TAPPA_RESET_DELIVERY=none`** — Q02 (hangi posta sağlayıcı, hangi bölge,
   hangi işleme sözleşmesi) cevapsız. Panelin kurtarma formu ekranda bunu söylüyor.
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
9. **KEK döndürme aracı YOK.** M8-02 *"KEK dönme prosedürü yazılı **ve
   yürütülebilir**: tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan araç
   var"* diyor. Böyle bir araç bu repoda yok, yani bugün bir KEK sızıntısının
   **yürütülebilir bir karşılığı yok**. Kartın açık kriteri.
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
12. **[Docker Hub anonim çekme bütçesi]** — 🔴 **BU MADDE ESKİ
    *[GHCR çekme kimliği ömürlü]* MADDESİNİN YERİNDE DURUYOR, VE O SINIR SAYILARAK
    DEĞİL KÖKÜNDEN KAPATILDI.** Eski madde şunu diyordu: `deploy.yml` `ghcr` sırrını
    her koşuda kendi `GITHUB_TOKEN`'ıyla yazıyor, o token iş bitince iptal ediliyor,
    ve bu node'un kubelet'i `imagePullCredentialsVerificationPolicy:
    NeverVerifyPreloadedImages` (KEP-2535) ile koştuğu için **kimlikle çekilmiş** bir
    imaj, kayıtlı kimliği olmayan bir pod'a **düğümde dururken bile** verilmiyor —
    yani her yeniden başlatma ve özellikle `kubectl rollout undo` bir `401` riskiydi.
    ✅ **Kapatan ölçüm zaten o maddenin içindeydi:** ayırt edici kontrol **public**
    bir imaj + `imagePullPolicy: Never` → **düğüm önbelleğinden açıldı, exit 0**.
    Public depoda kaydedilecek bir kimlik yoktur, yani kusur **tanım gereği**
    oluşamaz. İmajlar Docker Hub'a taşındı; `secret/ghcr`, onu yazan adım, iki pod
    spec'indeki `imagePullSecrets` ve takılmış-pod kurtarma adımı **AĞAÇTAN
    kaldırıldı** (backlog T43).
    🔴 **AĞAÇTAN — KÜMEDEN DEĞİL, VE BU AYRIM BU MADDENİN EN ÖNEMLİ SATIRIDIR.**
    Burada bir tur boyunca yalnız *"kaldırıldı"* yazdı; ölçüm (2026-08-16, salt
    okuma) bunu yalanlıyor: `secret/ghcr` **var** (18 sa), `deployment/tappa` →
    `ghcr.io/atknatk/tappa:sha-353897c6d5f6` + `imagePullSecrets: ghcr`, ve **üç
    ReplicaSet'in üçü de** `ghcr.io/...` + `ps=ghcr`. Geçiş kümeye ancak operatör
    adımı 4 tamamlandıktan **sonraki başarılı bir `deploy` koşusuyla** iner; o güne
    kadar **eski sınır (bayat çekme kimliği, `rollout undo` → 401) yürürlüktedir** ve
    *"Olay müdahalesi"* bölüm 2'nin dördüncü satırı onu taşır. Aynı ayrımın kalıbı
    bölüm 1'de zaten vardı (*"Ağaçtaki manifestler taşıyor; KÜMEDEKİ nesneler henüz
    taşımıyor"*) — burada yoktu.
    🔴 **YERİNE GEÇEN SAYILMIŞ SINIR — anonim çekme oran limiti.** Bu ağdan ölçüldü:
    `x-ratelimit-limit: 100;w=3600`, yani IP başına saatte 100 çekme. Tek node için
    geniş (bir deploy iki çekme harcar) ama artık **ürün imajları da** aynı bütçeden
    harcıyor, `postgres:17-alpine`'ın yanında. Düğüm imajı kaybederse — kubelet imaj
    GC'si, `crictl rmi`, düğüm yeniden kurulumu — bu bütçe pod'un açılmasıyla arasına
    girebilir. Belirtisi `toomanyrequests`/`429`'dur; *"Olay müdahalesi"* bölüm 2 onu
    ayrı bir satır olarak taşıyor. **Kapatılmadı, bilinçli:** kimlikli çekme limiti
    yükseltir ama `imagePullSecrets`'ı — yani bu maddenin kapattığı kusuru — geri
    getirir; pull-through önbelleği ise tek-node kurulum için ayrı bir bileşendir.
    ✅ **Depoların public OLMASI artık ilan değil, kapı.** `deploy.yml` push'tan
    hemen sonra — kümeye hiçbir şey uygulamadan — iki imajı da **kimliksiz** çekmeyi
    deniyor ve başaramazsa deploy orada durur (operatör adımı 4.3). On dejenere yol
    koşuldu, **yalnız biri geçiyor**: ikisi de gerçekten anonim çekilebiliyorsa.
    ⚠️ **Public KALMASI yine de operatörün elinde** — kapı her koşuda yeniden ölçer,
    ama bir depoyu sonradan private'a çeviren kimseyi engelleyemez; sonraki deploy'da
    kırmızı görünür.
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
18. **[sevk edilen commit'in kimliği herkese açık]** — 🔴 **§4.7 merceğiyle sayılan
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
    **Neden kabul edildi:** alternatifi private depo, o da KEP-2535 kimlik doğrulama
    kusurunu — yani **pod'un hiç açılamaması** riskini — geri getiriyor (sınır
    **[Docker Hub anonim çekme bütçesi]**). Açığa çıkan şey bir sır değil, bir
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
    🔴 **NEDEN SAYILMASI GEREKEN BİR SINIR:** dev/CI **dar**, üretim ve her geri
    yüklenmiş veritabanı **geniş** — yani **doğrulama ortamı üretimden katı** ve ayrım
    kusuru **gizler** yönde. `REVOKE` satırını unutan bir migration yerelde hiçbir fark
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

