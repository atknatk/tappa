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
| `k8s/00-namespace.yaml` | namespace + `pod-security enforce=restricted` | `deploy.yml` |
| `k8s/05-config.yaml` | sırsız ortam değişkenleri | `deploy.yml` |
| `examples/secret.example.yaml` | **şablon**, değer yok, adı **canlıyla çakışmaz** | **operatör** (kopyasını) |
| *(dosyasız)* `secret/ghcr` | GHCR çekme kimliği — `docker-registry` tipi | `deploy.yml` (kendi `GITHUB_TOKEN`'ıyla, **ömürlü** → adım 3 + sınır **[GHCR çekme kimliği ömürlü]**) |
| `examples/externalsecret.example.yaml` | **önerilen** yol: Infisical + external-secrets | **operatör** |
| `k8s/12-networkpolicy.yaml` | 5432'ye yalnız Tappa pod'ları | `deploy.yml` |
| `k8s/10-postgres.yaml` | StatefulSet + PVC (`local-path`) + headless Service | `deploy.yml` |
| `k8s/20-app.yaml` | Deployment + Service | `deploy.yml` |
| `k8s/30-migrate-job.yaml` | goose Job (`tappa_owner`) | `deploy.yml` |
| `k8s/40-ingress.yaml` | Ingress (`nginx`, `letsencrypt-prod`) | `deploy.yml` |
| `k8s/postgres-init/02-app-password.sh` | `tappa_app`'e **girişi açan** üretim script'i | ConfigMap içinde |
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
denetim ölçtü (2026-08-15). Sayı yanlıştı (NetworkPolicy'den sonra sekiz değil
**dokuz** nesne) ve daha kötüsü *"güvenli"* kelimesi Secret'tan **fazlasını** iddia
ediyordu. O komutun gerçekte yaptığı:

| | |
|---|---|
| örnek `Secret`'ı ezer mi | **hayır** — örnekler `deploy/examples/`'da (bu doğruydu) |
| `tappa-db-init` ConfigMap'i | **üretmez** — onu yalnız `deploy.yml` `--from-file` ile kurar → `tappa-postgres-0` kalıcı `ContainerCreating` (`configmap "tappa-db-init" not found`) |
| imaj etiketleri | **`:deploy-placeholder`** kalır → `ImagePullBackOff`, migration Job'ı **Failed** |
| çalışan bir kurulumda | rollout'u **kilitler** — Deployment'ı olmayan bir etikete geri alır |

Yani boş kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır.
⚠️ Bu teorik değil: bu turda kümede kazara bırakılmış tam olarak bu durum bulundu ve
temizlendi (bozuk StatefulSet + `ImagePullBackOff` + Failed Job + bağlı 20Gi PVC).

**Elle deploy etmek gerekiyorsa** `.github/workflows/deploy.yml`'in adımlarını sırayla
izle: namespace → `tappa-secrets` (elle) → **`ghcr` sırrı** → ConfigMap'ler
(`--from-file` dahil) → Postgres → NetworkPolicy → migration Job (**bekle**) →
Deployment → Ingress. Tek tek `apply` etmek güvenlidir; **dizini toptan** `apply`
etmek değildir.

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

**3. GHCR çekme kimliği (imaj private) — ELLE ADIM DEĞİL, ama sınırı var.**

`deploy.yml` `ghcr` sırrını **her koşuda kendisi yazar**, kümeye bir şey uygulamadan
önce, kendi `GITHUB_TOKEN`'ından:

```yaml
kubectl -n tappa create secret docker-registry ghcr \
  --docker-server=ghcr.io --docker-username="$GHCR_USER" --docker-password="$GHCR_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Çalışmasının sebebi dar: iş akışının `permissions:` bloğu `packages: write` veriyor
(read'i kapsar) ve bir workflow'un GHCR'a push ettiği paket **push eden depoya
bağlanır**, yani o deponun kendi token'ı geri çekebilir. İki imajı da bu iş, dakikalar
önce, bu token'la push etti.

🔴 **BU KALICI BİR ÇÖZÜM DEĞİL VE ÖYLE OKUNMAMALI. `GITHUB_TOKEN` iş bitince iptal
edilir.** Yürüyen şey token'ın yaşaması değil, iki ayrı olgunun üst üste binmesi:

| | |
|---|---|
| **çalıştığı koşul** | ilk çekme sır yazıldıktan **saniyeler sonra**, aynı işin rollout'unda olur · her iki manifest de `imagePullPolicy: IfNotPresent` ve etiket **değişmez** `sha-<12hex>` · yeniden başlatma, Secret hâlâ **o pod'un imajını çeken** token'ı taşırken güvenlidir |
| **çalışmadığı koşul (a)** | sonraki bir deploy Secret'ı **yeni** bir token'la ezdikten sonra o pod yeniden başlarsa — ya da `kubectl rollout undo` ile **eski** bir imaja dönülürse — kubelet kayıtlı kimliği bulamaz, çekmeyi yeniden dener ve **ölü token'la 401** alır |
| **çalışmadığı koşul (b)** | deploy bittikten **sonra** düğüm imajı düşürürse — kubelet imaj GC'si (`imageMinimumGCAge: 2m0s`, disk baskısı), `crictl rmi`, düğüm yeniden kurulumu — aynı sonuç |
| **her ikisinin bedeli** | `ImagePullBackOff`, pod açılmaz. Bu ürün için maliyeti sıradan değil: 04:00 vardiyası tap sayfasını **hiç** yükleyemez ve çevrimdışı kuyruk (M9-01) bile devreye giremez |

> 🔴 **BURADA ESKİDEN *"tek node, yani imaj düğümde önbellekli kalır → sonraki her
> yeniden başlatma kayda hiç gitmez"* YAZIYORDU. ÖLÇÜLDÜ, YANLIŞ (2026-08-16).** Bu
> node'un kubelet'i `imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages`
> ile koşuyor (KEP-2535): kubelet'in **kimlikle** çektiği bir imaj, yalnız o imaj için
> **kayıtlı** kimlik sunabilen pod'a veriliyor. Üç ölçüm: kimlik yok → `401` · hiç
> kullanılmamış kimlik → `401` · `imagePullPolicy: Never` → `ErrImageNeverPull`.
> Ayırt eden kontrol: **public** `postgres:17-alpine` + `Never` → **önbellekten
> açıldı, exit 0**. Tam gerekçe sınır **[GHCR çekme kimliği ömürlü]**'de.

**Kalıcı çare iki tane ve ikisi de kullanıcının işi.** Biri yapılana kadar yukarıdaki
sınır geçerlidir:

```bash
# (a) UZUN ÖMÜRLÜ PAT — aynı sır adı, aynı şekil. Yazılırsa deploy.yml onu her koşuda
#     kendi token'ıyla EZER; bu yolu seçersen deploy.yml'deki adımı kaldır.
# ⚠️ PAT KOMUT SATIRINA YAZILMAZ: kabuk geçmişine ve `ps`'e düşer. `read -rs` ile al.
read -rs -p "GHCR PAT (read:packages): " GHCR_PAT; echo
kubectl -n tappa create secret docker-registry ghcr \
  --docker-server=ghcr.io --docker-username=atknatk --docker-password="$GHCR_PAT" \
  --dry-run=client -o yaml | kubectl apply -f -
unset GHCR_PAT
```

**(b) Paketleri public yapmak.** ghcr.io/atknatk/tappa ve
ghcr.io/atknatk/tappa-migrate → Package settings → *Change visibility* → Public.
Sonrası çekme kimliği gerektirmez. 🔴 **Bunun API'si YOK — ölçüldü (2026-08-15):**
`PATCH /user/packages/container/tappa` → **404, böyle bir uç nokta yok**. Yalnız
arayüzden yapılır, yani hiçbir otomasyon bunu senin yerine kapatamaz. ⚠️ Public
paket, imajın **herkesçe indirilebilir** olması demektir; imaj `scratch` + iki dosya
ve sır taşımıyor (`deploy.yml` push öncesi bunu kapılıyor), ama karar yine de bilinçli
verilmeli.

`--docker-username` **kimlik doğrulamıyor**: ölçüldü (2026-08-15) — geçerli bir
token'la uydurma bir kullanıcı adı da `Login Succeeded` veriyor. Doğrulayan token.

**4. GitHub secret'ı — `KUBE_CONFIG`.**

```bash
base64 -w0 ~/.kube/config | gh secret set KUBE_CONFIG --repo atknatk/tappa
```

> ⚠️ **Bunu daraltın.** Tam cluster-admin bir kubeconfig'i CI secret'ına koymak,
> `main`'e push edebilen herkese cluster-admin vermektir. Doğrusu `tappa`
> namespace'inde bir ServiceAccount + Role (deployments/statefulsets/jobs/
> configmaps/services/ingresses üzerinde `get,list,create,patch,apply`, ve
> `namespaces` üzerinde yalnız `get`/`create`) ve o SA'nın token'ıyla üretilmiş bir
> kubeconfig. Bu iş **yapılmadı** ve M8-02'nin runbook yarısına devrediliyor.

**5. İlk deploy.** `main`'e push → `ci` yeşil → `deploy` kendiliğinden koşar.
Elle: Actions → `deploy` → Run workflow.

**6. İlk deploy sonrası — üç doğrulama (hiçbiri isteğe bağlı değil).**

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

**7. `/signup`'tan ilk işletmeyi aç.** İlk deploy **boş şemadır**, seed yoktur
(kullanıcı kararı). Sonra `/admin/legal`'e gir, reddetme sayfasının bastığı kendi
`admin_users.id`'ni `05-config.yaml` → `TAPPA_OPERATOR_ADMIN_IDS`'e yaz ve
`kubectl -n tappa rollout restart deployment/tappa`.

---

## Elle deploy / rollback

```bash
kubectl -n tappa set image deployment/tappa tappa=ghcr.io/atknatk/tappa:sha-<12hex>
kubectl -n tappa rollout status deployment/tappa
kubectl -n tappa rollout undo deployment/tappa      # bir önceki imaja
```

> 🔴 **`rollout undo` BU KÜMEDE SESSİZCE ÇALIŞMAYABİLİR — ve tam olarak ona ihtiyaç
> duyduğun anda.** Önceki imaj, **önceki** deploy'un `GITHUB_TOKEN`'ıyla çekilmişti;
> `secret/ghcr` o günden beri en az bir kez **yeni** bir token'la ezildi. Kubelet
> kimlik doğrulaması yaptığı için (KEP-2535, ölçüldü) bu bir **yeniden çekme**
> tetikler ve o token artık **ölüdür** → `401` → `ImagePullBackOff`, yani rollback
> **kesintiyi uzatır**. Ayrıntı ve ölçümler: sınır **[GHCR çekme kimliği ömürlü]**.
>
> **Geri almadan önce iki satırla kontrol et** — hedef imajın hâlâ çekilebilir olup
> olmadığını `--dry-run=server` söylemez, o yüzden rollout'u izle ve takılırsa
> **ileri** git (yeni bir deploy koşusu), geri değil:
> ```bash
> kubectl -n tappa rollout undo deployment/tappa
> kubectl -n tappa rollout status deployment/tappa --timeout=120s || \
>   kubectl -n tappa get pod -o wide   # ImagePullBackOff görüyorsan yukarıdaki sınır
> ```

> 🔴 **Rollback şemayı geri almaz.** `goose down` bu iş akışında **yoktur** ve
> bilinçlidir: geri alınabilir olmak (`-- +goose Down` dolu) ile *otomatik olarak
> geri alınmak* aynı şey değildir. Bir migration'ı geri almak, `transactions`
> immutable olduğu için (§4.3) veri kaybı anlamına gelebilir — el ile, ölçerek
> yapılır.

---

## Olay müdahalesi — belirti → sebep

> **Bu tablodaki her satır bu kümede fiilen ölçüldü.** Uydurma senaryo yok; bir
> belirti burada yoksa, o belirti **bu repoda henüz görülmedi** demektir ve teşhis
> ölçümle başlamalıdır, bu listeyi zorlayarak değil.
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
> 🔴 **AMA BU İKİSİ *"ŞU AN AYAKTA MIYIM"* DER, *"AYAĞA KALKABİLİR MİYİM"* DEMEZ —
> ve ikisi bu kümede farklı sorulardır.** Sınır **[GHCR çekme kimliği ömürlü]**
> gereği, çalışan pod'un imajı **kayıtlı** bir kimlikle çekilmiştir; `secret/ghcr` o
> pod doğduktan sonra bir deploy tarafından ezildiyse, o pod **yeniden başlarsa geri
> gelmez** ve bu sürerken hiçbir belirti vermez.
> ```bash
> scripts/verify-deployment.sh pull-credential tappa
> ```
> **Bu komut KANIT basar, HÜKÜM VERMEZ** — iki zaman damgası kümesini tek biçimde
> yan yana koyar ve kuralı yazar; karşılaştırmayı **sen** yaparsın. Çıkış kodu
> yalnızca *okuyabildim mi* sorusunu cevaplar: **`0` = kanıt basıldı (güvendesin
> DEMEK DEĞİL)** · **`2` = okunamadı, hiçbir kanıt basılmadı**.
>
> ⚠️ **NEDEN HÜKÜM YOK — bu bir tasarım tercihi değil, dört denetim turunun
> sonucu.** Aynı kontrol **üç kez** yazıldı ve **üçü de** koşulmamış bir yolda
> *"güvendesin"* dedi: v1 `zsh`'in reddettiği bir karşılaştırma yüzünden at-risk
> kümeye `ok` dedi · v2 `secret/ghcr` **hiç yokken** `ok` dedi (at-risk'ten **daha
> kötü** bir durum) · v3 *"yapısal olarak fail-closed"* diye **ilan edildi** ve
> **kırk satır aşağıda** çürüdü: ayrıştırılamayan bir `managedFields` girdisi sessizce
> **atlanıyordu**, yani en yeni damga okunamazsa eski bir damga kazanıyor ve sonuç
> yine `ok` çıkıyordu; ayrıca damga normalleştirici `+02:00` ofsetini **UTC sanıyordu**.
> `docs/plan/agent-brief.md`'nin ikinci durma kuralı burada devreye giriyor:
> *"Sayılmış bir açık, kapatıldığı İDDİA EDİLEN bir açıktan güvenlidir."*
> Tehlikeli olan zaman damgaları değil, **`ok` kelimesiydi** — yanlış bir hüküm
> operatörü **aramayı bırakmaya** ikna eder. Bu sürümün doğruluk iddiası da
> tartışmayla değil **sayımla** kanıtlanıyor: fonksiyonda hüküm basabilen yol sayısı
> **sıfır** (`ok`/`AT RISK`/`safe` geçişi: **0**), dokuz `return`'ün **dokuzu da**
> `return 2`, ve iki damga arasında **hiç** karşılaştırma yok.
> On altı vaka koşuldu: v3'ü düşüren iki kanal · Secret yok · `managedFields` boş ·
> girdi/satır sayısı uyuşmuyor · pod yok · yalnız boşluk · `kubectl` düşüyor ·
> zaman aşımı `rc=124` · `startTime` boş · pod adı boş · iki haneli yıl · `Z`
> taşımayan damga · üç pod'un ortası okunamaz → **on dördü de exit 2, hiçbiri kanıt
> basmadı**; iki okunabilir girdi kanıt bastı.
>
> **Bugünkü canlı çıktı** (pod `2026-08-15T22:20:44Z`, en yeni Secret yazımı
> `2026-08-16T08:38:41Z` — pod **daha erken**, yani o pod yeniden başlayamaz):
> çare bir sonraki **başarılı** deploy koşusudur.

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

### 2. `ImagePullBackOff` — kimlik düzeltilince **kendiliğinden toparlanmaz**

**Belirti.** `Failed to pull image ... 401 Unauthorized`, ya da
`ErrImageNeverPull: not present`. İmaj o node'da fiilen dururken bile olur.

**Sebep.** Sınır **[GHCR çekme kimliği ömürlü]**: kubelet kimlikle çekilmiş bir
imajı, **kayıtlı bir kimlik
sunamayan** pod'a vermez. Üstüne kubelet'in geri çekilme aralığı her denemede büyür,
yani sır tazelense bile pod dakikalarca fark etmez — **2026-08-15'te ölçülen süre
4 dk 39 sn** ve elle `kubectl delete pod` gerekti.

**Çare.** Yeni bir deploy koşusu (adım *"Recover pods stuck on a stale pull
credential"* bunu artık kendisi yapıyor). Deploy koşamıyorsan, kimlik tazeyken
pod'u yeniden yarat:
```bash
kubectl -n tappa delete pod <stuck-pod>     # SADECE ImagePullBackOff/ErrImagePull olanı
```
🔴 **`kubectl rollout undo` bu sınıra çarpar.** Önceki imaj, önceki deploy'un
token'ıyla çekilmişti; Secret o günden beri değişti, yani geri alma yeni bir çekme
tetikler ve **ölü token'la 401 alır**. Rollback'e güvenmeden önce sınır
**[GHCR çekme kimliği ömürlü]**'yü oku.

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

**Sebep.** İki tanesi ölçüldü, biri yapısal:
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

1. **[yedek YOK]** — **Yedek YOK, replika YOK.** `local-path`, tek node, `reclaimPolicy: Delete`.
   §1'in *"managed Postgres"*i ve M8-02'nin *"geri yükleme denenmiş"* kriteri
   **karşılanmıyor**. Kullanıcı bunu uyarıldıktan sonra seçti. **M8-06 pilotu
   başlamadan kapatılmalı**: off-node `pg_dump` CronJob'ı + **provalı** bir geri
   yükleme. Ayrıntı `k8s/10-postgres.yaml` başında.
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
8. **Kubeconfig secret'ı daraltılmadı** — gerekçe ve devir notu *"Operatörün
   yapması gerekenler"* bölümünün **4. adımında** (`KUBE_CONFIG`). ⚠️ Burada
   eskiden yalnız *"madde 4 yukarıda"* yazıyordu; bu listenin içinde okunduğunda
   **sınır 4**'e (HSTS) işaret ediyor gibi görünüyordu — B1'in kapattığı sınıfın
   aynısı, o yüzden hedefi adıyla yazıldı.
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
12. **[GHCR çekme kimliği ömürlü]** — 🔴 **"Çözüldü" değil, "şu koşulda çalışır".**
    `deploy.yml` `ghcr` sırrını her koşuda kendi `GITHUB_TOKEN`'ıyla tazeliyor ve o
    token **iş bitince iptal edilir**. **Çalışır:** ilk çekme sır yazıldıktan saniyeler
    sonra, aynı koşunun rollout'unda olur.
    🔴 **BURADA YAZAN İKİNCİ YARIM YANLIŞTI VE 2026-08-16'DA ÖLÇÜLEREK ÇÜRÜTÜLDÜ.**
    *"imaj düğümde kalır, sonraki her pod yeniden başlatması kayda hiç gitmez"*
    **doğru değil.** Bu node'un kubelet'i
    `imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages` ile koşuyor
    (KEP-2535, 1.35'ten beri varsayılan): kubelet'in **kimlikle** çektiği bir imaj,
    yalnız o imaj için **kayıtlı** bir kimlik sunabilen pod'lar tarafından
    kullanılabilir; başkasına imaj **düğümde yokmuş gibi** davranır. Canlı pod'un o
    sırada koştuğu etiketle üç ölçüm: kimlik yok → `401` · hiç kullanılmamış bir
    kimlik → `401` · `imagePullPolicy: Never` → `ErrImageNeverPull, "not present"`.
    Ayırt eden kontrol: **public** `postgres:17-alpine`, `imagePullPolicy: Never` →
    düğüm önbelleğinden **açıldı, exit 0**. Yani önbellek çalışıyor; reddeden şey
    kimlik doğrulaması. **Sonuç:** yeniden başlatma yalnız Secret **o pod'un imajını
    çeken** token'ı hâlâ taşırken güvenli. Sonraki bir deploy Secret'ı **yeni** bir
    token'la ezdikten sonra, eski imaja dönmek (`kubectl rollout undo`) çekmeyi
    yeniden tetikler ve **ölü token'la 401 alır** — yani en çok ihtiyaç duyulan anda,
    olay anında. **Çalışmaz'ın eski hâli de duruyor:** düğüm imajı düşürürse (kubelet
    imaj GC'si, `crictl rmi`, düğüm yeniden kurulumu) aynı sonuç.
    **Hafifletme (çare değil):** `deploy.yml` artık *"Recover pods stuck on a stale
    pull credential"* adımıyla, kimlik tazelendikten hemen sonra **yalnız kubelet'in
    `ImagePullBackOff`/`ErrImagePull` bildirdiği** pod'ları yeniden yaratıyor —
    `Running` bir pod'a asla dokunmaz. Bu, 2026-08-15'te elle `kubectl delete pod`
    gerektiren 4 dk 39 sn'yi kapatır; **alttaki sınırı kapatmaz.**
    **Kalıcı çare iki tane ve ikisi de
    kullanıcının işi:** `read:packages` yetkili uzun ömürlü bir PAT, ya da paketleri
    public yapmak (**API'si yok**, yalnız arayüz — ölçüldü). Adım 3'te ikisi de yazılı.
    Bu madde, biri yapılana kadar açık kalır.
13. **[her yeni iş yükü kendi beklemesini taşır]** — 🔴 **Bu namespace'te Postgres'e
    bağlanan HER YENİ İŞ YÜKÜ kendi bekleme adımını taşımak zorunda.**
    `12-networkpolicy.yaml` uygulanıyor, ama k3s yeni bir pod'un adresini kuralın izin
    kümesine **eşzamansız** yazıyor: ölçüldü, izin verilen etiketi taşıyan beş taze
    pod'un **beşi de** ilk denemesinde reddedildi, 0,2–1,0 sn sonra kabul edildi
    (kural silinmiş kontrol: üç pod, üçü de sıfırıncı denemede bağlandı). İlk işi
    veritabanına bağlanmak olan bir süreç bu yarışı **kaybeder**;
    `restartPolicy: Never` taşıyan bir Job ise **her zaman** kaybeder, çünkü her
    yeniden deneme yeni adresli yeni bir pod'dur. Bugün migration Job'ı ve sunucu
    Deployment'ı `wait-for-postgres` initContainer'ı taşıyor; buraya eklenecek bir
    CronJob (örn. sınır **[yedek YOK]**'un `pg_dump`'ı) **aynısını taşımazsa sessizce
    ve tekrarlanabilir biçimde düşer**. Belirti `connection refused`'dır, zaman aşımı
    değil — bkz. *"Olay müdahalesi"*.
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
15. **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** — `deploy.yml`'in *"Recover
    pods stuck on a stale pull credential"* adımı `kubectl get pod`'a dayanıyor; o
    çağrı düşerse (RBAC, apiserver 5xx, zaman aşımı) adım **kırmızı olur ve deploy
    orada durur**.
    ✅ **Bu maddenin ikinci yarısı KAPATILDI, sayılmadı:** `get` ile `delete` arasında
    pod kaybolursa (Job'ın `ttlSecondsAfterFinished` toplayıcısı, ya da yerine koyan
    bir denetleyici) `delete` **NotFound ile exit 1** veriyordu ve `set -e` adımı
    öldürüyordu — yani deploy, *"kurtarmak istediğim pod zaten gitmiş"* gibi **iyi
    huylu** bir sebeple ölüyordu. Ölçüldü: bayraksız `Error from server (NotFound)`,
    **exit 1**; `--ignore-not-found` ile **exit 0**. Bayrak eklendi (aynı dosya
    `delete job tappa-migrate`'te onu zaten kullanıyordu). **`get`'in düşmesi hâlâ
    kırmızı ve bu doğru** — aşağıdaki gerekçe onun içindir. Bu **doğru varsayılan** — kubectl okuyamıyorsa alttaki `apply`
    adımları da koşamaz, ve *"kurtarılacak bir şey yok"* diye devam etmek tam olarak
    bu turda kapatılan fail-open kusurudur. ⚠️ **Ama artık riski sayılıyor:** adım
    `secret/ghcr` **ezildikten sonra**, hiçbir `apply`'dan **önce** duruyor, yani
    orada düşen bir deploy kümeyi *"yeni token, eski iş yükü"* durumunda bırakır —
    sınır **[GHCR çekme kimliği ömürlü]**'nün tarif ettiği pencerenin ta kendisi.
    Böyle bir koşudan sonra `scripts/verify-deployment.sh pull-credential tappa`
    koş. 🔴 **O komut HÜKÜM VERMEZ — `AT RISK` diye bir çıktı YOKTUR**; iki zaman
    damgası kümesini basar, karşılaştırmayı sen yaparsın: **en yeni `secret/ghcr`
    yazımından ÖNCE başlamış her pod yeniden başlayamaz**, ve çare yeni bir
    **başarılı** deploy koşusudur. ⚠️ Burada eskiden *"`AT RISK` diyorsa"* yazıyordu
    ve bu **sınıfın beşinci örneğiydi**: hüküm koddan kaldırıldı ama **runbook'ta
    yaşamaya devam etti**, yani operatör hiç gelmeyecek bir kelimeyi bekleyip
    *"demek ki sorun yok"* diye kümeyi at-risk hâlde bırakabilirdi — üstelik bu
    komutun **en çok işe yaradığı** anda. Bu turda yazdığım mekanik tarama da
    yakalayamazdı: tarayıcı **anahtar atıflarını** sayıyordu, **hüküm sözcüklerini**
    değil — yani tarayıcıyı bir önceki kusurun şekline göre yazmıştım.
16. **[migration arızası artık ~10× geç bildiriliyor]** — `wait-for-postgres` her
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
