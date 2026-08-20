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

> 🔴 **BU BAŞLIK ALTINDA HİÇBİR ŞEY SEÇİLMEDİ.** Yeni bir taşıma bağımlılığı
> CLAUDE.md §1 gereği **kullanıcı onayı** ister; bir ajan onu kendi başına
> seçemez. Burada yazılı olan şey **seçenekler ve bilinme dereceleri**dir.

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
   listesinin sekiz maddesi yerinde duruyor.
2. **iOS'un ~20 sn sınırı altında bir röle turunun gerçekten yetişip
   yetişmediği.** Kaba hesap yetiyor diyor; **ölçüm yok**. C yolunu tek başına
   açacak/kapatacak sayı budur.
3. **NTAG 424 DNA'nın kendi tarafında bir oturum zaman aşımı olup olmadığı.**
   AN12196'da bulamadım; veri sayfasına birincil kaynaktan erişilemedi
   (nxp.com JS'siz istemcilere 404 döndürüyor). ISO 14443-4 tarafı sorun
   çıkarmıyor ama **çip tarafı ölçülmedi**.
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
| Dosya **yazma** izni | **anahtarla kilitli** | Aynı örnekte (rev. 1.8 §6.9 / rev. 2.0 §5.9) `FileAR.Write = 0x0` (anahtar 0). Yazma serbest kalırsa duvardaki plaketin NDEF'ini herkes değiştirebilir. |
| `K_SDMFileRead` | plaket-başına **rastgele** AES-128 | ADR 0003 md. 3 (Q06). `crypto/rand`; hiçbir şeyden türetilmez. |
| Fabrika varsayılan anahtarı | **değiştirilir** | ADR 0003 md. 5: *"Bu varsayılan anahtar üretimde **asla** kullanılmaz"*; `K_SDMFileRead` varsayılandan değiştirilir. ⚠️ ADR **yalnız bunu** emrediyor; kalan uygulama anahtarlarının da kişiselleştirilmesi AN12196 rev. 1.8 §6.16'nın (rev. 2.0: §5.16) **tavsiyesidir** (*"highly recommended to configure all the Application Keys during personalization"*) — FAZ B'de karara bağlanır. |
| URL parametre adları | **tam olarak** `tag`, `ctr`, `cmac` | ADR 0003 md. 1. Kodda tek yerde sabit: `internal/sun/params.go` (`paramTag`/`paramCtr`/`paramCMAC`). Başka bir ad = ayrıştırma hatası. |
| URL biçimi | `https://<host>/t?tag=<14 hane>&ctr=<6 hane>&cmac=<16 hane>` | ADR 0003 md. 1. |

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

**Teslim.** Çipler tedarikçiden **fabrika varsayılanı** anahtarlarla gelir. Encode
sırasında en azından `K_SDMFileRead` bizim ürettiğimiz rastgele anahtarla değiştirilir
(ADR 0003 md. 5); kalan uygulama anahtarları için yukarıdaki tablonun *"Fabrika
varsayılan anahtarı"* satırına bak.
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
   kopyayı siler. Encode aracı bu ikisini **aynı süreçte, art arda** çağırmak
   **zorundadır** (araç yazıldığında; bugün yok — bölüm başındaki uyarı).
2. **Veritabanı yalnız sarmalıyı görür.** `tags.aes_key_ref` = 44 bayt
   (`nonce(12) ‖ ciphertext(16) ‖ gcm_tag(16)`), ADR 0003 md. 4.
3. **Uygulama rolü onu yazamaz.** Migration 00013 `tappa_app`'e `tags` üzerinde
   **sütun listeli** UPDATE veriyor (`location_id`, `last_ctr`, `status`,
   `retired_at`, `replaced_by`) — `aes_key_ref` **listede yok**. Satırı
   `tappa_owner` yükler.
4. **Panel plaket yaratamaz.** `db/queries/tags.sql` **hiç INSERT taşımıyor** ve
   bu bir eksiklik değil karardır (kullanıcı kararı 2026-08-08: *"Tappa encodes
   the plaque and loads the row; the panel only binds it"*). Plaket doğuran tek
   yol, bu runbook'un tarif ettiği encode akışıdır — **ve o akışı koşan araç henüz
   yazılmadı**, yani bugün üretimde encode edilmiş plaket **yoktur**.
5. **Mekanik tarama.** `scripts/redline-check.sh` R7 iki şeyi arıyor: repoda
   gömülü anahtar dosyası (`*.pem`, `*.key`, `*.aes`, `secrets/*`) ve sır taşıyan
   log çağrısı. ⚠️ Geçmesi ihlal olmadığını **kanıtlamaz** (CLAUDE.md §4).
6. **Emsal: yalnız SARMALI blob çıktıya çıkar.** `test/fixtures/seedkeys` KEK'i
   ortamdan okur, asla basmaz; stdout'a yalnız sarmalı değeri taşıyan SQL yazar.
   Encode aracının çıktısı **aynı şekli** almalıdır.

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

> #### 🔴 Q08 AÇIK — ENCODE EDİLEN HOST GERİ ALINAMAZ
>
> SUN URL'si **domaini taşır** ve domain **henüz alınmadı** (`tappa.mt` /
> `tappa.io`, EUIPO taraması da yapılmadı — `open-questions.md` Q08).
>
> Çipe yazılan NDEF, o çipin anahtarı olmadan **değiştirilemez**; anahtarı olsa
> bile her plaket için **tek tek, fiziksel olarak** yeniden yazmak demektir. Yani
> **yanlış host'la encode edilmiş bir plaket = sahada plaket değişimi.** ADR 0003
> girişinde bu maliyeti *"yanlış modda veya yanlış anahtar stratejisiyle"* encode
> için adlandırıyor; **host üçüncü hâlidir** ve ADR'de yazmıyor — burada yazıyor.
>
> **Kural: Q08 kapanmadan üretim plaketi encode edilmez.** Deneme/geliştirme
> plaketleri encode edilebilir, ama duvara **çıkmaz** ve `status='unassigned'`
> kalır.

### Encode edilen satırın durumu

Encode biten bir plaketin satırı **`status='unassigned'`** ile yüklenir,
`location_id` **NULL**. Bu, migration 00013'ün envanter modelidir: Tappa plaketi
**encode eder ve yükler**, müdür panelden **duvara bağlar**. `active` yazmak,
kutuda duran bir plaketi hizmette göstermek olur — ve `internal/sun`'ın akışı
`active` olmayan her plaketi §5 satır 1'den reddeder, yani yanlış durum sessiz
kalmaz ama **yanlış yönde** de yanılmaz.

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
3. **En az bir fiziksel etiketle uçtan uca doğrulama** (kart kriteri 3): encode →
   duvara benzet → gerçek telefonla tap → kayıt panelde görülür.
4. **Gerçek çiple replay denemesi** (M8-04 kartı bunu istiyor): aynı tap URL'si
   ikinci kez gönderilir → **reject** beklenir; `tags.last_ctr` tek adım ilerlemiş
   olmalıdır.
5. **Q11 — iPhone Safari çerez ömrü** (`open-questions.md`: *"gerçek cihaz turu
   M8-05'te"*). Ölçülecek olan: **sunucunun yazdığı httpOnly** çerez ITP altında
   1 yıl yaşıyor mu (7 günlük kırpma **JS ile yazılan** çerezlere ait — doğrulanması
   gereken tam olarak bu ayrım). *"Telefon seni tanır"* vaadi buna dayanıyor.
6. **Fabrika varsayılanlarının gerçekten değiştiği doğrulanır**: encode sonrası
   varsayılan anahtarla kimlik doğrulama **başarısız** olmalı.
7. **Yazma izninin gerçekten kilitli olduğu doğrulanır**: kimlik doğrulamasız bir
   NDEF yazma denemesi **reddedilmeli**.
8. **Baskı provası** — A5 ölçüleri, QR kenar uzunluğu ve sessiz alan **fiziksel
   provada** doğrulanır; doğrulanınca skill `tappa-brand`'in ilgili bloğu
   *"ölçüldü"* olarak güncellenir.

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
```bash
umask 077                                   # yeni dosyalar 0600 doğsun
# 🔴 install -m 600 /dev/null ÖNCE: umask VAR OLAN bir dosyanın iznini
#    değiştirmez ve > bir symlinki İZLER. /tmp bu makinelerde drwxrwxrwt, ad
#    öngörülebilir ve dosya rotasyondan önce yok — yani ad önceden yaratılabilir.
#    Ölçüldü: çıplak > ile hedef mode 666 ve symlink KALIYOR; install ile
#    symlink düz 0600 dosyayla DEĞİŞİYOR. Aynı teknik iki satır aşağıda eski KEK
#    için zaten kullanılıyordu; yalnız birine uygulanmıştı.
install -m 600 /dev/null /tmp/kek_new.b64
openssl rand -base64 32 > /tmp/kek_new.b64  # YENİ KEK — ekrana bakma
# ESKİ KEK: bugün kümede duran değer. Emanetten/sır deposundan al ve dosyaya yaz;
# terminale yazdırma. (Küme kopyasının parmak izini operatör adımı 2deki sha256
# önekiyle karşılaştırabilirsin — değerin kendisini basmadan.)
# 🔴 cat > KULLANMA: etkileşimli tty girdiyi EKRANA YANKILAR. Ama read -rs -p
#    DE KULLANMA — ölçüldü, zsh'te -p "coprocess'ten oku" demek: read anında
#    1 dönüyor, && zinciri kırılıyor, printf hiç koşmuyor (dosya 0 BAYT kalıyor)
#    ve girdi TÜKETİLMEDİĞİ için yapıştırılan KEK bir komut olarak çalışıp
#    command not found: <KEK> ile EKRANA BASILIYOR. Aşağıdaki biçim POSIXtir ve
#    bash ile zshte AYNI davranır (ikisinde de ölçüldü).
install -m 600 /dev/null /tmp/kek_old.b64
printf 'ESKI KEK (yapistir, Enter): '
stty -echo; IFS= read -r K; stty echo; printf '\n'
printf '%s' "$K" > /tmp/kek_old.b64
unset K
wc -c < /tmp/kek_old.b64      # 44 (base64) olmalı; 0 ise DUR, yankı riski var
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
# t olmalı. f ise DUR: o oturum parkın tamamını ne görebilir ne yazabilir.
```

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
# log_statement = none  ve  log_min_duration_statement = -1  olmalı.
```
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

```bash
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'     # >= 1 OLMALI
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'   # 0 OLMALI
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

```bash
# Parola DSN'de değil, PGPASSFILE'da. Böylece temizlik bir DESEN EŞLEŞTİRMESİ
# değil, tek bir dosyanın silinmesi olur (eski sürüm ~/.pgpasste literal "HOST"
# arıyordu ve gerçek bir dosyada HİÇBİR ZAMAN eşleşmiyordu — yani temizlik
# koşulsuz bir no-optu ve yine de "kalan owner satiri: 0" basıyordu).
umask 077
export PGPASSFILE="$(mktemp)"
trap 'rm -f "$PGPASSFILE"' EXIT INT TERM HUP
printf '%s:5432:tappa:tappa_owner:%s\n' "<host>" "<parola>" > "$PGPASSFILE"
export OWNER_DSN="postgres://tappa_owner@<host>:5432/tappa?sslmode=disable"   # parola YOK

export TAPPA_TAG_KEK_FILE=/tmp/kek_old.b64      # bugünkü (sızmış) KEK
export TAPPA_TAG_KEK_NEW_FILE=/tmp/kek_new.b64  # yeni KEK

./scripts/rotate-kek.sh              # DRY RUN: hiçbir şey uygulamaz, planı ve sayıları gösterir
echo "rc=$?"

# 🔴 YAZMAK ICIN --apply ZORUNLU. Ciplak cagri bir DRY RUN yapar. Bu satir bir tur
#    boyunca "uygular" yorumuyla ciplak duruyordu ve OLCULDU: rc=0, hicbir sey
#    uygulanmadi, 77.215 satirin tamami ESKI KEK altinda kaldi -- ve tablo o
#    sifiri "uygulandi" diye okuyordu. Operator Adim 3e gecerse her tap 500 doner.
./scripts/rotate-kek.sh --apply      # UYGULAR (geri alinamaz)
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

```bash
export TAPPA_ROTATE_ALLOW_UNOPENABLE="<raporun verdiği tam sayi>"   # TIRNAK ŞART
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

```bash
# (a) Parkta eski KEK altında satır kaldı mı — hiçbir şey UYGULAMADAN.
#     İKİ KEK de hâlâ ortamda olmalı (araç ikisini de ister; biri boşsa exit 1).
#     Açılamayan satırı olan bir parkta TAPPA_ROTATE_ALLOW_UNOPENABLE de gerekir.
./scripts/rotate-kek.sh --dry-run; echo "rc=$?   # 0 beklenir, hiçbir şey uygulanmaz"
# Beklenen: re-sealed under the new KEK ..... 0 ve already under the new KEK
#           = toplam satır sayısı. (Eskiden burada "re-sealed 0" yazıyordu; araç
#            o dizeyi HİÇ basmıyor, yani literal bir grep her zaman boş dönerdi.)

# (b) Sarmal uzunlukları — 44 dışındaki her sayı bir zarf DEĞİLDİR.
psql -X "$OWNER_DSN" -c "SELECT octet_length(aes_key_ref) AS len, count(*) FROM tags GROUP BY len ORDER BY count DESC;"

# (c) GERÇEK BİR TAP — tek uçtan uca kanıt, ve HÂLÂ İKİ KEK YÜKLÜYKEN koşulur.
#     Bir plakete dokun; kayıt düşmeli. Bu aşamada 500 alıyorsan adım 3e GEÇME.
#     Önce rolloutun oturduğundan emin ol, yoksa eski poda düşen bir tapi
#     yeni podun kusuru sanarsın:
kubectl -n tappa rollout status deployment/tappa --timeout=180s
```

### Adım 3 — eski KEK'i düşür (ROTASYON BUDUR)

Yalnız **adım 2 exit 0** verdiyse **ve** doğrulama (a)(b)(c) temizse.
`tappa-secrets`'tan **`TAPPA_TAG_KEK_PREVIOUS`'u kaldır**, sonra:

```bash
kubectl -n tappa rollout restart deployment/tappa      # sır düzenlemek POD'U YENİDEN BAŞLATMAZ
kubectl -n tappa rollout status  deployment/tappa --timeout=180s
```

**KAPI — yine POZİTİF olgu, aynı tablo:**

```bash
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=closed'   # >= 1 OLMALI
kubectl -n tappa logs deployment/tappa | grep -c 'kek_rotation_window=open'     # 0 OLMALI
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

```bash
# YALNIZ yukarıdaki tablo "KAPANDI" dediyse: operatörün kabuğu ve diski temizlenir.
unset TAPPA_TAG_KEK TAPPA_TAG_KEK_PREVIOUS
# 🔴 "ÜZERİNE YAZARAK SİL" DİYE BİR ADIM YOK — bu satır eskiden öyle diyordu ve
#    YANLIŞTI. Bu platformda rm -P belgeli bir NO-OPtur ve **0 döner**
#    (man rm: "This flag has no effect."), yani || shred dalına HİÇ geçilmez;
#    shred de bu makinede yok. Ölçüldü. Dosyalar UNLINK ediliyor, üzerine
#    yazılmıyor. scripts/rotate-kek.sh kendi geçici dosyaları için bunu
#    sondalayıp söylüyor; burada da doğrusu yazılıyor.
#    Gerçekten üzerine yazmak istiyorsan: şifreli bir birim ya da RAM disk kullan.
rm -f /tmp/kek_old.b64 /tmp/kek_new.b64
command -v shred >/dev/null && echo "not: shred var, istersen kullanabilirsin" || \
  echo "not: bu makinede shred YOK ve rm -P bir no-op; dosyalar unlink edildi, uzerine yazilmadi"
# $PGPASSFILE adım 2deki trap tarafından zaten silindi; kalıcı bir parola dosyası
# bu prosedürde HİÇ yaratılmıyor (tek mekanizma kuralı).
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

```bash
kubectl -n tappa get cronjob      # boşsa bu adım bugün için geçersiz
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
| Kalan **yapıştırılan** bloklarda aynı tehlike | 🔴 **SAYILDI, İMKÂNSIZ DEĞİL:** bu bölümde hâlâ **13 yapıştırılan `bash` bloğu** var (sır girişi, `kubectl` adımları, doğrulama). Tehlike **karakter düzeyinde** kaldırıldı — yorumlarda **0 backtick, 0 tek apostrof** — ve `TestRunbook_PasteableBlocksCarryNoShellHazardInComments` bunu tutuyor. ⚠️ Ama bu bir **özellik**, yapısal bir imkânsızlık değil: yeni bir blok yeni bir backtick getirebilir, ve o testi geçersiz bir yorum hâlâ yapıştırılabilir. Kontrol: gerçek `zsh -f -i`'de README şeklindeki backtick'li bir yorum **çalıştı** (işaret dosyası yazıldı); temizlenmiş blokların **hepsi** `bash -n` **ve** `zsh -n` altında 0 veriyor. ⚠️ **Testin kapsamı M8-05'te genişledi, M8-03'te bir kez daha genişledi, ve hâlâ dosyanın tamamı DEĞİL:** artık **üç** dilim taranıyor — bu bölüm · *"Plaket encode"* (bugün orada 0 blok var, yani tarama bir sürüklenme freni) · *"Gözlemlenebilirlik — M8-03"* (5 blok; iki tehlikeli yorum satırı orada sevk edildi ve iki dilimlik ağ onları görmemişti). Dosyanın kalan **32** yapıştırılabilir bloğu taranmıyor; sayısı ve içindeki tehlike satırları **"Kabul edilmiş sınırlar"** listesinde yazılı |
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

```bash
# node ve bölge
kubectl get nodes -o wide
# -> k8s-1.fsn1.private  Ready  control-plane  v1.35.4+k3s1  10.0.0.10  Ubuntu 24.04.4
# ⚠️ topology.kubernetes.io/region ETİKETİ YOK (ölçüldü: boş). Bölge kanıtı
#    node ADI (fsn1 = Hetzner Falkenstein, Almanya) ve M8-02 kartında kaydedilen
#    144.76.158.60 adresidir — bir etiket değil.
kubectl get nodes -o jsonpath='{.items[0].metadata.labels}'

# toplayıcı gerçekten bizi okuyor mu
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
# -> "containerLogMaxSize":"10Mi"
#    "containerLogMaxFiles":5
#    "containerLogMaxWorkers":1
#    "containerLogMonitorInterval":"10s"
```

**Node kopyası: konteyner başına 10 MiB × 5 dosya = 50 MiB, sonra en eskisi silinir.**
Bu bir **boyut** sınırıdır; hiçbir katmanda **zaman** sınırı yoktur.

Bugünkü hacimle bu ne demek:

```bash
kubectl -n tappa logs $(kubectl -n tappa get pod -l app.kubernetes.io/component=server \
  -o jsonpath='{.items[0].metadata.name}') | wc -lc
# ⚠️ BURAYA BEKLENEN BİR ÇIKTI YAZILMIYOR, BİLEREK. Bu sayı pod her yeniden
#    başladığında sıfırlanır ve pod yaşıyla birlikte büyür; bir tur onu
#    "3 satır, 489 bayt" diye dondurdu, sonraki ölçüm 4 satır / 557 bayt verdi.
#    Yapıştırıp kendi çıktını oku; karşılaştırılacak bir referans YOK.
```

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

```bash
# Erişim kaydı hizi: iki olcum arasindaki fark / gecen sure.
# (Once bir kez, 10 dakika sonra bir kez; ikisinin farki gunluk hizi verir.)
POD=$(kubectl -n tappa get pod -l app.kubernetes.io/component=server \
  -o jsonpath='{.items[0].metadata.name}')
kubectl -n tappa logs "$POD" | wc -lc
kubectl -n tappa logs "$POD" | grep -c http.request
# 50 MiB / (kayit boyutu) = kac yanit sigar; hiz x gun = ne zaman dolar.
```

Ve GDPR açısından önemli olan yarı bu **değil**, şu: log satırları IP ve çalışan
id'si taşır (§4.2, §4.7), Q13'ün silme akışı `employees` üzerinde bir UPDATE'tir
ve **log'a hiç ulaşmaz** — dolayısıyla saklama süresi ne kadar uzunsa o kadar
kötüdür, ne kadar bilinmezse o kadar kötüdür.

⚠️ **VE BU SINIRSIZLIK YANILTICI, ÇÜNKÜ İKİNCİ BİR SİLİCİ VAR: DEPLOY.** Pod
silindiğinde kubelet `/var/log/pods/<uid>` dizinini kaldırır, yani her rollout
öncekinin log'unu atar. Ölçüldü:

```bash
kubectl -n tappa get rs        # 7 ReplicaSet = 7 rollout
kubectl -n tappa get pods -o name
# -> pod/tappa-6fd5489dcb-q5b4l      <- UYGULAMA: bundan YALNIZ BİRİ var
#    pod/tappa-migrate-rq76l          (tamamlanmış Job)
#    pod/tappa-postgres-0             (StatefulSet)
# ⚠️ ÇIKTI ÜÇ SATIR. Bir tur burada "(YALNIZ BİRİ)" yazıp tek satır gösteriyordu;
#    "yalnız biri" olan şey UYGULAMA POD SATIRIDIR, komutun çıktısı değil. Diğer
#    ikisi aşağıdaki §4.7 kapsam notunun konusu.
```

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

> **TESLİMAT KANALI YOK, VE BU DÜRÜSTÇE YAZILIYOR.** Bu kurallar bugün bir yere
> **gönderilmiyor**: uyarının **nereye gideceği** `Q28 (a)`'da açık, ve SigNoz'un
> uyarı kurulumu bu repodaki bir dosyaya değil, o kümedeki başka bir ürünün
> konfigürasyonuna bağlı.
> **Sevk edilen şey şudur: altı sinyalin altısı da log'dan HESAPLANABİLİR, alan adları
> testle pinlenmiştir, ve eşikler yazılıdır.** Kuralı bir hedefe bağlamak
> operatörün işi ve aşağıdaki sorgular olduğu gibi yapıştırılabilir.
>
> **Sahibi:** kümeyi işleten operatör. **Neye bağlı:** `Q28 (a)` — uyarı hedefi
> (**teslimat**) · `Q28 (b)` — log saklama süresi (**saklama**; bir uyarıyı
> araştırmak için log'un hâlâ orada olması gerekir) · operatör eylemi
> `log-retention-signoz` (SigNoz'un logları gerçekten tuttuğu süre, `Q28 (b)`'nin
> ölçülmemiş yarısı).
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

**Altı sinyal**, dört olaydan hesaplanır (aşağıdaki tablo altı satırdır; bir tur
boyunca burada *"beş"* yazıyordu ve 6. kural eklendikten sonra belge kendisiyle
çelişiyordu):

| # | Sinyal | Olay (`msg`) | Filtre | Eşik / pencere | Ne anlama gelir | İlk bakılacak yer |
|---|---|---|---|---|---|---|
| 1 | **`reject` oranı sıçraması** | `tap.decision` | `verdict = "reject"` | 15 dk'lık pencerede `reject` / toplam **> %10** **ve** en az 5 reject | Ya bir plaket `lost`/`retired`/`unassigned` (§5 satır 1), ya SUN doğrulaması düşüyor (satır 2), ya bir hesap deaktive (satır 4). Üçü de farklı arıza. | `matched_sid` alanı hangi satır olduğunu söyler — `sys:tag-not-active`, `sys:sun-invalid`, `sys:employee-deactivated` |
| 2 | **`flag` kuyruğu birikmesi** | `tap.decision` | `verdict = "flag"` | 60 dk'lık pencerede **> 20** flag, **veya** aynı `matched_sid` ile **> 10** | Kanıt yetersiz kalıyor ve §4.6 gereği kayıtlar müdür kuyruğuna düşüyor. En sık sebebi bir lokasyonun statik IP'sinin değişmesi. | `matched_sid`; sonra panelin onay kuyruğu ve `locations.static_ips` |
| 3 | **güvenlik olayı — İKİ AYRI SEBEP** | `tap.security_alert` | olayın kendisi | **≥ 1** olay = uyarı. Pencere yok. | 🔴 **Bu olay İKİ yoldan doğar ve ikisi farklı işler.** (a) §5 satır 4 — canlı bir oturum, **kapatılmış bir hesapla** plakete dokundu (`matched_sid = sys:employee-deactivated`): ya ayrılan biri telefonunu kullanıyor, ya bir oturum çalınmış. (b) §5 satır 1 — **kayıp bildirilen bir plakete** dokunuldu (`matched_sid = sys:tag-not-active`, tag durumu `lost`): plaket duvardan sökülmüş ya da kopyalanmaya çalışılıyor. ⚠️ `retired` ve `unassigned` **uyarı üretmez** — o ikisi rutin yaşam döngüsü. | **Önce `matched_sid`'e bak, hangi yol olduğunu O söyler.** (a) için `employee_id`; (b) için `tag_uid` ve `plaques` ekranındaki plaket geçmişi. Her iki durumda `audit_log`'da aynı adla kanıt satırı var (`tap.security_alert`) |
| 4 | **şüpheli `ctr` sıçraması** | `tap.decision` | `ctr_gap > 10` | tek olay bile bakılmaya değer; **> 50** acil | Çip, sunucunun görmediği kadar okundu — URL biriktirmenin (A1) tek gözlemlenebilir izi (Q21). | `tag_uid` yok bu olayda: `matched_sid = base:ctr-gap-review` ile `transactions` satırını bul |
| 5 | **5xx oranı** | `http.request` | `level = "ERROR"` (yani `status >= 500`) | 5 dk'lık pencerede **> 5** olay **veya** toplam isteğin **%1**'i | Sunucu bozuk. Panik `middleware.Recoverer` tarafından 500'e çevrilir ve **bu olayda görünür**. 🔴 **Sağlık sondaları bu olayın DIŞINDADIR** — aşağıdaki bloğa bak; `/readyz`'in 503'ü tasarımdır ve bu kuralı ateşlemez, ama `/readyz`'den gelen bir **500** ateşler. | `route` + `request_id`; aynı `request_id` ile o isteğin diğer satırları |
| 6 | **hazırlık kaybı** | `readiness.lost` | olayın kendisi | **≥ 1** olay = uyarı. Pencere yok. Kapanışı `readiness.regained`'dir. | Veritabanı cevap vermiyor, `/readyz` 503 dönüyor ve pod rotasyondan düşüyor (M8-01). 🔴 **M8-03 4. TUR: kayıt artık sürücünün METNİNİ taşımıyor.** Bir tur boyunca taşıyordu ve bu, `health.go`'nun aynı metni kimliksiz bir HTTP çağıranından esirgeme gerekçesiyle çelişiyordu (rol · veritabanı · host · port · TLS duruşu — ölçüldü; **parola yok**). Yerine iki alan geçti: `err_class` **kapalı kümedir** (`server` · `timeout` · `canceled` · `dns` · `dial` · `other`) ve `err_cause` adres taşımayan en özgül sebeptir — `server` için **SQLSTATE** (yanlış parola `28P01`, olmayan veritabanı `3D000`), `dial` için çağrının kendi kelimesi (`connect: connection refused`), `dns` için `no such host`. **Kaybedilen:** sürücünün cümlesi; `other` sınıfı sebep taşımaz. | `err_class`, sonra `err_cause`; ardından bölüm 1'deki `connection refused` ve bölüm 5'teki DNS akışı |

**Yapıştırılabilir SigNoz/ClickHouse tarzı filtreler** (alan adları koddaki
sabitlerdir ve `TestObservability_AlertSignalNames` ikisinin aynı kaldığını
denetler; **filtrelerde geçen her `sys:`/`base:` adının `internal/policy`'de
gerçekten var olduğunu** `TestObservability_EverySidInTheAlertRulesExists`
denetler — aşağıdaki nota bak):

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
    ⚠️ **İkinci sıfır kapı, aynı sınıftan ve aynı şekilde sayılıyor:**
    `cmd/rotatekek/script_test.go` yapıştırılabilir blokların yorumlarını tarıyor ama
    **tüm dosyayı değil, üç dilimi** okuyor ("KEK döndürme", "Plaket encode" ve
    M8-03'te eklenen "Gözlemlenebilirlik").
    Ölçüldü: dosyada **50** yapıştırılabilir `bash` bloğu var; **13**'ü döndürme
    diliminde, **0**'ı encode diliminde, **5**'i gözlemlenebilirlik diliminde, kalan
    **32**'si taranmıyor ve o 32 blokta **43 yorum satırı** var; bunların **21**'i
    **26 tehlike isabeti** taşıyor. Taramayı dosyanın tamamına açmak o
    21 satırı **kırmızıya** çevirirdi — başka kartlara ait bir düzeltme işi; bu yüzden
    **kapatılmadı, sayıldı**. Kapatan tur, o 21 satırı düzelten turdur.
    ⚠️ **BU SAYILAR CANLI — donduruldukları için ölçüm komutları yanlarında duruyor.**
    2. turda **ikisi de yanlıştı**: toplam **45** yazıyordu, ölçüm **50** verdi
    (M8-03 beş blok ekledi); ve *"kalan 32"* yazıyordu, oysa o an yalnız iki dilim
    taranıyordu, yani taranmayan **37**'ydi. Bugün 32 **doğru**, çünkü üçüncü dilim
    o beş bloğu kapsıyor — ama **aynı sayı, farklı sebeple**. Yanlış toplam tam
    olarak üçüncü dilimin **hiç taranmadığını** gizliyordu; bu yüzden artık ölçümü
    yeniden üretecek komut da burada:

    ````bash
    # toplam yapıştırılabilir blok
    grep -c '^```bash' deploy/README.md
    # dilim başına dağılım (satır sayısı, blok, tehlike) — testin kendi logu
    go test ./cmd/rotatekek/ -run PasteableBlocks -v
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
25. **[uyarıların TESLİMAT KANALI yok]** — M8-03'ün üçüncü kriteri **hesaplanabilirlik**
    olarak sevk edildi: **altı** sinyalin altısı da log'dan bir filtre ile çıkıyor, alan
    adları `TestObservability_AlertSignalNames` ile, filtrelerdeki politika adları
    `TestObservability_EverySidInTheAlertRulesExists` ile pinlendi, eşikler yazılı.
    **Hiçbiri bir yere gönderilmiyor** — hedef `Q28 (a)`'da (**teslimat**) açık ve
    kurulum bu repodaki bir dosyada değil. Sahibi: kümeyi işleten operatör.
    ⚠️ Bu madde bir tur boyunca `Q12`'ye atıf veriyordu; `Q12` **barındırmadır** ve
    uyarı hedefi hakkında hiçbir şey söylemez. Sorunun kendisi (`Q28`) o yanlış atıf
    ölçülünce açıldı.
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
