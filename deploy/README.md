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
izle: namespace → Secret (elle) → ConfigMap'ler (`--from-file` dahil) → Postgres →
NetworkPolicy → migration Job (**bekle**) → Deployment → Ingress. Tek tek `apply`
etmek güvenlidir; **dizini toptan** `apply` etmek değildir.

---

## Operatörün yapması gerekenler (sırayla, bir kez)

**1. DNS — ve bu adım en kritik olanı.**

`tappa.everva.com.tr` için **A kaydı → `144.76.158.60`**, Cloudflare proxy
**KAPALI** (gri bulut / "DNS only").

> ⚠️ `everva.com.tr` Cloudflare'de ve **proxy'li bir joker kayıt var** — ölçüldü
> (2026-08-15): uydurma bir alt alan da aynı iki Cloudflare adresine çözülüyor,
> yani `tappa.everva.com.tr` **bugün zaten** Cloudflare üzerinden cevap veriyor.
> Proxy açık kalırsa uygulama her istemciyi bir Cloudflare adresi olarak görür:
> §5'in IP kanıtı (100 güven puanının 50'si) **hiç kimse için** doğru olamaz ve
> panel giriş bütçesi tüm müşteriler için tek adrese çöker (backlog T30).
> Gerekçenin tamamı `k8s/40-ingress.yaml` başında.

Doğrulama (kayıt açıldıktan sonra):

```bash
dig +short tappa.everva.com.tr                     # 144.76.158.60 olmalı
curl -sS -o /dev/null -D - https://tappa.everva.com.tr/ | grep -i 'cf-ray\|^server'
# cf-ray varsa kayıt hâlâ proxy'li demektir.
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

**3. GHCR çekme kimliği (imaj private).**

```bash
# read:packages yetkili bir PAT ile, BİR KEZ.
# ⚠️ PAT KOMUT SATIRINA YAZILMAZ: kabuk geçmişine ve `ps`'e düşer. `read -rs` ile al.
read -rs -p "GHCR PAT (read:packages): " GHCR_PAT; echo
kubectl -n tappa create secret docker-registry ghcr \
  --docker-server=ghcr.io --docker-username=atknatk --docker-password="$GHCR_PAT"
unset GHCR_PAT
```

Deploy iş akışının `GITHUB_TOKEN`'ı kullanılamaz: iş bitince ölür, bir saat sonra
yeniden zamanlanan pod imajı çekemez.

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

> 🔴 **Rollback şemayı geri almaz.** `goose down` bu iş akışında **yoktur** ve
> bilinçlidir: geri alınabilir olmak (`-- +goose Down` dolu) ile *otomatik olarak
> geri alınmak* aynı şey değildir. Bir migration'ı geri almak, `transactions`
> immutable olduğu için (§4.3) veri kaybı anlamına gelebilir — el ile, ölçerek
> yapılır.

---

## Kabul edilmiş sınırlar — hepsi bilinçli, hiçbiri kaza

1. **Yedek YOK, replika YOK.** `local-path`, tek node, `reclaimPolicy: Delete`.
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
6. **CI'nin Go'su 1.26.5, imajınki 1.26.6.** `make audit` T31 yüzünden kırmızı;
   **sevk edilen ikili** 1.26.6 ile derlendiği için o altı stdlib açığını
   taşımıyor. CI pini kullanıcının Go kurulumudur.
7. **`sslmode=disable`** — bağlantı bu node'un kendi köprüsünden çıkmıyor
   (tek node, pod↔pod `cni0`). Yönetilen bir Postgres'e taşınırsa `verify-full`
   gerekir; CA paketi imajda **zaten var**.
8. **Kubeconfig secret'ı daraltılmadı** (madde 4 yukarıda).
9. **KEK döndürme aracı YOK.** M8-02 *"KEK dönme prosedürü yazılı **ve
   yürütülebilir**: tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan araç
   var"* diyor. Böyle bir araç bu repoda yok, yani bugün bir KEK sızıntısının
   **yürütülebilir bir karşılığı yok**. Kartın açık kriteri.
10. **Uygulamanın `:8080`'i küme içinden erişilebilir.** NetworkPolicy yalnız
    Postgres'e yazıldı; sunucunun `httpGet` probe'ları **node'dan** geliyor ve dar bir
    kural onları da düşürebilir — bu kümede NetworkPolicy'nin *uygulandığı* bile
    `apply` etmeden doğrulanamadı. Şiddeti düşük (aynı içerik zaten internete açık,
    prod'da çerez `Secure`, panel düz HTTP girişini T38 gereği zaten reddediyor); atlanan
    şey TLS ve HSTS. `k8s/12-networkpolicy.yaml` ölçümü ve doğrulama komutunu taşıyor.
11. **`DATABASE_URL`'e elle owner DSN'i yazılırsa ürün itiraz etmez.** `config.Load`
    yalnız **eşitliği** reddediyor. Kapatan şey bir **deploy kapısı**
    (`deploy.yml` → *"the running server must not be connected as the migration
    role"*, `pg_stat_activity` üzerinden), yani **deploy anına** özgü; deploy sonrası
    değiştirilen bir değeri yakalamaz. Süreç içi kontrol bilinçli olarak
    **yapılmadı** — gerekçesi kartta, ölçümüyle.
