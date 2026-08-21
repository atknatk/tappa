# M8 — Deploy & pilot

**Amaç.** Ürünü sahaya çıkarmak: tek VPS (AB bölgesi) + managed Postgres,
plaketlerin fiziksel encode'u ve KF St Julians'ta BioTime ile paralel pilot.

**Bittiğinde:** Gerçek çalışanlar gerçek plaketlere dokunuyor, kayıt karşılaştırma
tablosu çıkmış durumda (satış slaytı da olur).

---

## M8-01 — Paketleme

- **Bağımlılık:** M6 tamam
- **Commit:** `build: add production packaging`

**Kabul kriterleri.**
- `make build` → `CGO_ENABLED=0`, statik, `-trimpath`, sürüm/commit gömülü.
- Statik varlıklar ve şablonlar binary içinde (`web/embed.go`) — runtime dosya
  bağımlılığı yok.
- Migration ayrı adım (`tappa_owner`), uygulama başlangıcında **otomatik migrate yok**
  (yanlış rolle çalışma riski).
- Sağlık uçları: `/healthz` (canlı) + `/readyz` (DB'ye bağlı).
- `time/tzdata` gömülü — sistem tzdata'sı olmayan imajda vardiya hesabı çökmesin.

> **Kart düzeltmesi (2026-08-15, M8-01 uygulaması sırasında).** Beş kriterin **üçü
> zaten sevk edilmişti** ve kart bunu bilmiyordu; ölçümler (hepsi `5a86f9b`'de,
> uygulamadan **önce**):
>
> | Kriter | Ölçüm | Kartın bilmediği |
> |---|---|---|
> | `CGO_ENABLED=0` · `-trimpath` | `Makefile:72-73` | — |
> | sürüm/commit gömülü | `go version -m bin/tappa` → `vcs.revision=5a86f9b9…`, `vcs.time`, `vcs.modified` | **Zaten gömülüydü.** Go'nun `-buildvcs`'i yapıyor ve `-s -w` onu **soymuyor**; eksik olan **okuma**ydı (`grep -rn "Version\|BuildCommit" cmd/ internal/config/` → **0 eşleşme**). `-ldflags -X` **gereksiz**. |
> | runtime dosya bağımlılığı yok | üretim kodunda `os.ReadFile`/`os.Open`/`http.Dir` → **0 eşleşme** | Karşılanıyordu, ama **hiçbir şey tutmuyordu**. |
> | otomatik migrate yok | `go list -deps ./cmd/tappa \| grep -c goose` → **0** | Aynı: karşılanıyordu, tutan yoktu. |
> | `time/tzdata` gömülü | `go list -deps ./cmd/tappa \| grep -c '^time/tzdata$'` → **1** | 🔴 Ama import **`internal/domain/tap/tzdata.go:23`**'teydi — yani *tap karar motorunun* bir dosyası. `cmd/tappa`'nın bağımlılıkları değişse ya da o satır silinse **sessizce kaybolurdu**. |
>
> **Bu yüzden görevin ağırlığı "yapmak" değil, "bozulunca kırmızı vermesini
> sağlamak" oldu** (`cmd/tappa/packaging_test.go`, `cmd/tappa/serving_test.go`).
> Gerçekten yeni olan üç şey: **(a)** `cmd/tappa` `time/tzdata`'yı **kendi** import
> ediyor (idempotent; artık artefaktın kendi garantisi), **(b)** derleme kimliği
> `internal/buildinfo` ile **okunuyor** ve **açılışta tek satır** log'lanıyor —
> kirli ağaçtan derlenmişse **WARN** (bir commit'e geri izlenemez), **(c)**
> `/readyz` var (`internal/handler/health.go`) ve `/healthz` artık **HEAD**
> cevaplıyor.
>
> **Kartın söylemediği üç karar, ölçümleriyle:**
> 1. **Sürüm/commit nereye çıkacak?** Ölçüldü ve **herkese açık uca konmadı**:
>    `/healthz` gövdesi tam olarak `ok`, `/readyz` gövdesi `ready`/`not ready` —
>    ikisini de **tam eşitlikle** tutan testler `TestHealthz_AnswersGetAndHead` ve
>    `TestReadyz_AnswersTheDatabasesAnswer`. Commit hash'i kimliksiz bir uçta basmak,
>    saldırgana **hangi kodun** koştuğunu söyler; §4.7'nin sır listesinde değil ama
>    bedava da değil. Operatörün **iki ayrıcalıklı** kanalı zaten var: açılış log
>    satırı ve artefaktın kendisi (`go version -m`). Aynı gerekçeyle `-version`
>    bayrağı **eklenmedi** — `go version -m` aynı beş alanı zaten basıyor.
>    ⚠️ **Sürüm ≠ commit:** `git tag` **boş**, yani semver yok; toolchain'in
>    türettiği sözde-sürüm (`v0.0.0-20260815011242-5a86f9b9bb53`) dürüst cevaptır.
> 2. **`/readyz` `marketing.go`'nun "ölçümsüz" argümanını kırıyor mu?** Evet, ve
>    argüman **yeniden yazıldı** (o dosyada dipnot): `/healthz` ve `/static/*`'ın
>    sınıfı *"hiçbir şeye dokunmuyor"*, `/readyz` ise **havuza dokunuyor**. Limiter
>    **konmadı** (adres başına harita, 100k'da tamamen sıfırlanıyor — kimliksiz bir
>    uçta saldırgan sayısıyla büyüyen bellek); yerine **önbellek**: 200 eşzamanlı
>    istek → **tam 1** `Ping` (pozitif kontrol: pencere sıfırlanınca 200 → 200;
>    `TestReadyz_ABurstCostsOneQuery`), ve kesinti başına **1 ERROR + 1 INFO** satır
>    — durum **değişimi** log'lanıyor, durum değil (`TestReadyz_AnOutageCostsTwoLogLines`).
>    Ayrıca çağıranın iptali probe'a **geçmiyor** (`context.WithoutCancel`): aksi
>    halde sekmesini kapatan biri bu sürecin hazır-değil kanısını çevirip log'unu
>    doldurabilirdi (`TestReadyz_ACallerWhoHangsUpCannotFlipReadiness`).
> 3. **§4.5: hangi rol, hangi tenant bağlamı?** `pgxpool.Ping` **hiçbir tablo
>    okumuyor**. Ölçüldü (`TestReadyz_ATableReadCouldNotAnswerThisQuestion`, gerçek
>    Postgres): `SELECT count(*) FROM transactions` → **owner 200 473 satır**,
>    `tappa_app` **tenant bağlamı olmadan 0**. Yani tablo okuyan bir readiness
>    kontrolü **sağlıklı** bir veritabanında da 0 döner — başarısı hiçbir şey
>    kanıtlamaz. §4.7: 503 gövdesi **sabit**; sürücünün gerçek hatası (ölçüldü:
>    ``failed to connect to `user=… database=…`: 127.0.0.1:1 …``) yalnız **süreç
>    log'una** gidiyor.
>
> **2. tur — denetim yirmi mutasyon koşturdu, yedisi yeşil döndü ve altısı aynı
> sınıftı: argüman taşıyan bir sabiti/sırayı hiçbir şey tutmuyordu.** Kapatıldı ve
> her biri için mutasyonun artık kırmızı verdiği gösterildi: `defaultReadyTTL`
> (1 sa yapılınca ölü DB'de bir saat `ready` derdi) ve `defaultProbeTimeout` (20 dk
> yapılınca asılı DB'de her çağıran kilitte beklerdi — eski test **kendi** 50 ms'ini
> set ettiği için varsayılanı hiç sürmüyordu) artık **davranışla** pinli;
> `db.Ping`'in tablo okumadığı **bakım veritabanına** (`postgres`) karşı ölçülüyor —
> orada `transactions` **yok**, yani tablo okuyan bir ping 42P01 ile düşer;
> `logBuild`'in **DB dial'ından önce** koşması, bozuk DSN'li bir açılışın çıktısında
> kimlik satırının bulunmasıyla tutuluyor; `nosniff` artık **karşılaştırılmıyor,
> iddia ediliyor** (GET==HEAD karşılaştırması iki boş dizeyi eşit sayıyordu).
>
> **Ve tzdata kriteri "binary'de var"dan "gerçekten çözüyor"a yükseldi
> (2026-08-15, ölçüm):** `/usr/share/zoneinfo` **olmayan** `alpine:3` içinde, yalnız
> blank import'la ayrılan iki program — importsuz **üç zone'da da FAIL** (`unknown
> time zone Europe/Malta`, exit=3), importlu **hepsi OK** (`04:00 local = 02:00 UTC`,
> exit=0). Komut ve çıktının tamamı `cmd/tappa/packaging_test.go`'da, ilgili testin
> yanında **yeniden üretilebilir** biçimde yazılı. **Süite eklenmedi**, ve gerekçe
> ölçüm: bu repoda hiçbir test docker çalıştırmıyor (`grep` → 0), macOS'ta üretilen
> artefakt darwin/amd64 olduğu için **ikinci bir çapraz derleme** gerekiyor ve
> `alpine:3` **çekilmek zorunda kaldı** — `make test`'in bugün hiç taşımadığı bir ağ
> bağımlılığı. (CI docker koşuyor — `make up` — yani ileride eklenebilir; bu bilinçli
> bir genişletme kararı olur.)
>
> **3. tur — `tappa-security-auditor` ONAY; kapatılan tek madde yapısaldı.** Denetçi
> ölçtü: `handler.NewHealth(data, …)` ile **somut `*db.DB`** ilk kez
> `internal/handler`'a giriyordu. Tüketici arayüzü tek metotluydu ama **arayüz değeri
> nesnenin tamamını taşır**, ve o sarmalayıcı **tenant bağlamsız çözücüleri** de
> sunuyor (`internal/db/resolve.go`) — üstelik paket `internal/db`'yi zaten import
> ediyor (`adminreset.go`), yani `h.probe.(*db.DB).GetAdminSessionByTokenHash(…)`
> yazmanın önünde yalnız **disiplin** vardı. Çözüm: `readinessProbe` artık bir
> **fonksiyon tipi**, wiring `handler.NewHealth(data.Ping, …)`. Bağlı metot **tek bir
> çağrı** taşır, geri dönülecek bir dinamik tip yok. **İki ağ, ölçüldü (2026-08-15):**
> havuzu geri geçirmek **derlenmiyor** (`cannot use data (variable of type *db.DB) as
> handler.readinessProbe value`, `go build` exit=1) · `Health`'e bir `*db.DB` **alanı**
> eklemek derleniyor ama `TestHealth_HoldsACapabilityAndNotAPool` kırmızı veriyor.
>
> ### Devir — bu kartta kapatılmayanlar (ölçümleriyle)
>
> 1. **`Allow` iki ayrı başlık satırı olarak gidiyor, ve `OPTIONS` cevaplanmıyor.**
>    Ölçüm (2026-08-15, **gerçek artefakt**, `curl -s -o /dev/null -D -`):
>    `POST|PUT|OPTIONS|DELETE /readyz` ve `POST /healthz` → **405**, gövde 0 bayt,
>    başlıklar **`Allow: GET`** *ve ayrı satırda* **`Allow: HEAD`**.
>    ⚠️ **Denetim raporu bunu *"`Allow: GET`, HEAD'i saymıyor"* diye kaydetmişti;
>    yeniden ölçüm bunu düzeltti** — HEAD **ilan ediliyor**, yalnızca virgülle
>    birleştirilmiş tek satır olarak değil. Kusur bu yüzden daha dar: *"ilk `Allow`
>    başlığını oku"* diyen istemciler (Go'nun `Header.Get`'i dahil) yalnız `GET`
>    görür, ve `OPTIONS` 405 alır (204 + `Allow` değil). chi'nin varsayılan davranışı;
>    **bu diff'in ürettiği bir kusur değil** — `/` de aynı iki satırı veriyor (M7-01).
>    → **M8-02**: router'a virgüllü tek `Allow` üreten ve `OPTIONS`'ı cevaplayan bir
>    `MethodNotAllowed` handler'ı.
> 2. **`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir WARN.** Bu
>    turun kendi artefaktı öyle: `version=v0.0.0-…+dirty … modified=true`. Sinyal
>    **var, filtrelenebilir ve testle tutuluyor**, ama `TAPPA_ENV=prod` ile kirli bir
>    binary'yi **reddeden ne bir kod ne bir test** var. → **M8-02** runbook adımı ya da
>    `make build` kapısı.
> 3. **Kilit probe boyunca tutuluyor.** 3. tur denetimi ölçtü (asılı DB, soğuk
>    önbellek, **300 eşzamanlı istek**): hepsi 503, **maks 2,001 sn** — yani
>    `defaultProbeTimeout` fiilen bağlıyor — ve aynı anda `/healthz` **0,0008 sn**.
>    Tetikleyici **gerçek bir kesinti**; saldırgan üretemez (önbellek + `WithoutCancel`)
>    ve pencere kapanınca herkes önbellekten anında döner. → **M8-03** (alarm/
>    gözlemlenebilirlik), davranış değişikliği değil.
> 4. **⚠️ DOĞRULANAMADI, ne bulgu ne temiz:** *"havuz doyduğunda `/readyz` 503 verip
>    tüm replikaları rotasyondan düşürür"* zinciri **ölçülemedi**. Denetçi
>    `pool_max_conns=1` + `LOCK TABLE` ile denedi; **kimliksiz hiçbir istek yolu havuz
>    bağlantısını ≥2 sn tutmuyor** (`/t` → 400/303, ikisi de <2 ms, DB'ye hiç
>    gitmeden). Yani bugün bu senaryoyu **kurmanın yolu bulunamadı**; kapasite
>    planlaması yapılırken (M8-02) yeniden bakılmalı.
>
> **T29'un `/healthz` yarısı bu kartta kapatıldı** (uptime monitörleri HEAD'ler ve
> 405'i *"servis bozuk"* okur). `/t`, `/activate` ve panel **hâlâ 405** — farklı
> patlama yarıçapı, T29'da açık kalıyor. **T28 (HSTS) ve T30 (`TAPPA_TRUSTED_PROXIES`)
> bu kartta ele alınmadı:** ikisi de **dağıtım** kararı (TLS'i nerede sonlandırdığımız,
> ters vekilin CIDR'i) ve yeri **M8-02**'nin runbook'u.

---

## M8-02 — Barındırma

- **Bağımlılık:** M8-01 · Q08 · Q12
- **Commit:** `docs(ops): document deployment runbook`

**Kabul kriterleri.**
- VPS + managed Postgres, **AB bölgesi**, aylık maliyet hedefte (~€30-50).
- TLS (HSTS dahil); SUN URL'si `https://` — domain Q08 ile netleşmiş.
- `tappa_app` / `tappa_owner` rol ayrımı üretimde de geçerli; uygulama
  **asla** migration rolüyle bağlanmıyor.
- Yedek: otomatik, geri yükleme **denenmiş** (denenmemiş yedek yedek değildir).
- Sırlar ortam değişkeninde, repoda değil; KEK dönme prosedürü yazılı **ve
  yürütülebilir**: tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan
  araç var. Yürütülemeyen bir runbook, KEK sızıntısı anında hiçbir işe yaramaz.
- **Deploy penceresi vardiyalara göre seçiliyor.** KM Pastry & Bakery 04:00'te,
  Meat Production 05:00'te giriş yapıyor; sunucu kapalıysa sayfa hiç yüklenmez
  ve çevrimdışı kuyruk (M9-01) bile devreye giremez — §4.6 altyapı katmanında
  karşılıksız kalır. Kesintisiz deploy ya da ilan edilmiş bakım penceresi.
- **DPA ve saklama politikası (Q23):** Tappa↔müşteri veri işleme sözleşmesi
  (KF/KM veri sorumlusu, Tappa veri işleyen), saklama süreleri ve silme/
  anonimleştirme akışı (Q13) yazılı ve imzalanmış.
- Runbook: deploy, rollback, migration, olay müdahalesi.

> **Kart düzeltmesi (2026-08-15, M8-02 uygulaması sırasında — PAKETLEME + KÜME
> MANİFESTLERİ yarısı).** Bu tur `Dockerfile`, `.dockerignore`, `deploy/k8s/*` ve
> `.github/workflows/deploy.yml`'i getirdi. **Kartın hedefi değişti ve bunu yazmak
> gerekiyor:** kart *"tek VPS + managed Postgres"* diyor; hedef **k3s v1.35.4, tek
> node** (`k8s-1.fsn1.private`, Hetzner fsn1, `144.76.158.60`) ve **düz Postgres**
> oldu (kullanıcı kararı, uyarıldıktan sonra).
>
> **Karşılanmayan iki kriter — sessizce kabul EDİLMEDİ, manifestin başına ve
> `deploy/README.md`'ye yazıldı:**
>
> | Kriter | Durum | Ölçüm |
> |---|---|---|
> | *"managed Postgres"* (§1) | **karşılanmıyor** | `kubectl get sc` → tek sınıf **`local-path`** (varsayılan, `WaitForFirstConsumer`, `reclaimPolicy: Delete`), replika yok |
> | *"Yedek: otomatik, geri yükleme **denenmiş**"* | **karşılanmıyor** | yedek **yok**; denenecek bir şey de yok. M8-06 pilotu bir haftalık paralel kayıt üretiyor ve o hafta §4.3 gereği yeniden kurulamaz → **pilot öncesi kapatılmalı** |
> | DPA / Q23, KEK döndürme aracı, runbook | **bu turda yok** | kartın ikinci yarısı |
>
> **🔴 EN AĞIR BULGU KARTTA HİÇ YOKTU: `everva.com.tr` CLOUDFLARE'DE VE JOKER KAYIT
> PROXY'Lİ.** Ölçüldü (2026-08-15, komutlarıyla):
> `dig +short tappa.everva.com.tr` → **`104.21.72.109`, `172.67.181.173`** (uydurma
> bir alt alan da aynı ikisi) · `dig +short NS everva.com.tr` → `*.ns.cloudflare.com`
> · `curl -sS -o /dev/null -D - https://argocd.everva.com.tr/` → **`server: cloudflare`,
> `cf-ray: …`**. Yani hedef host **bugün zaten** Cloudflare üzerinden cevap veriyor.
> ingress-nginx'in ConfigMap'i **boş** (`kubectl -n ingress-nginx get cm
> ingress-nginx-controller -o jsonpath='{.data}'` → boş), yani `use-forwarded-headers`
> varsayılan `false` ve nginx `X-Forwarded-For`'u `$remote_addr` ile **EZİYOR**.
> Proxy açık kalırsa uygulama her istemciyi bir **Cloudflare kenar adresi** olarak
> görür: §5'in IP kanıtı (50/100 güven puanı) **hiç kimse için** doğru olamaz, panel
> giriş bütçesi (T30, 120/10 dk/adres) **tüm müşteriler için tek adrese** çöker, ve
> T40'ın aynası doğar (Cloudflare aralığı girilmiş bir mekân tüm interneti
> *"ağ kanıtı"* sayar — üstelik değişmez bir satıra). **Karar: bu host için DNS-only
> (gri bulut), A → `144.76.158.60`.** Elenenler ve gerekçeleri `deploy/k8s/40-ingress.yaml`
> başında; paylaşılan ingress ConfigMap'ini değiştirmek ~20 başka uygulamanın istemci
> adresi çözümlemesini değiştirirdi.
>
> **T30 (`TAPPA_TRUSTED_PROXIES`) ÖLÇÜLDÜ, TAHMİN EDİLMEDİ → `10.42.0.1/32`.**
> ingress-nginx burada **`hostNetwork: true` bir DaemonSet** (`podIP=10.0.0.10`,
> hostPort 80/443), yani pod ağına node'un ağ ad alanından giriyor:
> `kubectl -n ingress-nginx exec ds/ingress-nginx-controller -- ip route get 10.42.0.5`
> → **`dev cni0 src 10.42.0.1`**. `10.42.0.1` cni0 köprüsünün kendisi; flannel pod
> adreslerini `10.42.0.2`'den dağıtıyor (podCIDR `10.42.0.0/24`), yani **hiçbir pod bu
> adresi alamaz**. `/24` yazmak kümedeki ~30 namespace'in her pod'una XFF sahteciliği
> yetkisi verirdi; `/32` yalnız bu node'un yönlendirme yolunu güvenilir sayar.
>
> **T28 (HSTS) — ölçüldü, ingress zaten gönderiyor.** Origin'e doğrudan (Cloudflare'ı
> atlayarak): `curl -sS -o /dev/null -D - -k --resolve argocd.everva.com.tr:443:144.76.158.60
> https://argocd.everva.com.tr/` → **`strict-transport-security: max-age=31536000;
> includeSubDomains`**. Bu Ingress onu miras alıyor. **`preload` set edilmedi** (geri
> alması aylar sürer) ve **uygulamanın kendi başlığını set etmesi hâlâ açık** — HSTS
> ingress-nginx'te per-controller bir ConfigMap seçeneği, per-Ingress annotation'ı
> **yok**, snippet annotation'ları da ≥1.9'da varsayılan olarak kapalı.
>
> **T31'in yarısı sevk edilen ikilide kapandı.** İmaj `golang:1.26.6-bookworm` ile
> derleniyor (CI hâlâ 1.26.5 pinli — o kullanıcının Go kurulumu). Ölçüldü:
> `go version -m` → `go1.26.6`.
>
> ⚠️ **Burada *"`make audit` hâlâ kırmızı"* yazıyordu ve BAYAT** (düzeltildi 2026-08-20,
> M8-04 FAZ B3): o cümle geliştirici makinesinin toolchain'ini sayan govulncheck'e
> bağlıydı ve toolchain o günden beri güncellendi. Bugün ölçüldü: `go version` →
> **`go1.26.7`**, `make audit` → **exit 0** (`govulncheck exit=0`, `redline-check
> exit=0`). **T31 kapandı.**
>
> **Ölçerek verilen beş karar (brief'in *"ölç ve seç"* dediği yerler):**
> 1. **Nihai imaj `scratch`** — 22,3 MB (16,1 MB ikili + 216 KB CA paketi). Dosya
>    sistemi tam olarak iki dosya. **tzdata M8-01'in sondasıyla, ama `alpine:3`'te
>    değil SEVK EDİLEN İMAJIN İÇİNDE** kanıtlandı: importsuz kontrol **üç zone'da da
>    FAIL** (`unknown time zone Europe/Malta`, exit=3), importlu **hepsi OK**
>    (`04:00 local = 02:00 UTC`, exit=0). **`ca-certificates` GEREKLİ, ölçüldü:** aynı
>    ikili sertifikasız `scratch`'te `x509: certificate signed by unknown authority`,
>    sevk edilen imajda `VIES TLS: OK` (VIES çağrısı Q09 gereği best-effort, yani
>    eksik paket çökme değil **sessiz bozulma** üretirdi).
> 2. **Migration `Job`, initContainer DEĞİL.** Üç ölçülebilir fark: `tappa_owner`
>    DSN'i internete açık pod'un spec'ine girmez · initContainer **her** pod
>    başlangıcında koşar (CrashLoopBackOff = sıkı döngüde DDL denemesi) · N replika =
>    N eşzamanlı goose. Ayrıca `deploy.yml` Job'ı **bekliyor**, yani şema ilerlemeden
>    yeni ikili servis etmiyor.
> 3. **`replicas: 1`, ve bu bir varsayılan değil ölçüm.** `internal/domain/legal`
>    paket dokümanı: *"A SECOND process would serve the previous text until it
>    restarted."* İki replika = aynı hostname'den **iki farklı gizlilik politikası**.
>    `maxSurge:1 / maxUnavailable:0` kartın *"kesintisiz deploy"*ını karşılıyor.
> 4. **`deploy.yml` ayrı workflow (`workflow_run`), `ci.yml`'e job DEĞİL.** Güvenlik
>    gerekçesi: GitHub `workflow_run` iş akışlarını **daima varsayılan dalın**
>    kopyasından koşturur, yani bir PR bu dosyayı değiştirip koşturamaz. `ci.yml`
>    **hiç değişmedi** (`git diff --stat .github/workflows/ci.yml` → boş).
> 5. **Sır pod'a ANAHTAR ANAHTAR veriliyor, `envFrom` ile değil.** Kartın
>    *"uygulama **asla** migration rolüyle bağlanmıyor"* kriterinin mekanizması bu:
>    sunucu pod'unun ortamında `DATABASE_MIGRATE_URL` **yok**. Ölçüldü: değişken
>    **yokken** süreç config doğrulamasını geçiyor (`db: ping` hatasına kadar
>    ilerliyor); **eşitken** açılışta reddediliyor.
>
> **M8-01'in devir listesinden bir madde kapandı, biri kapanmadı.**
> ✅ **Madde 2** (*"`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir
> WARN"*): `deploy.yml` artık imajı push'tan **önce** açıp `vcs.revision`i deploy
> edilen SHA ile karşılaştırıyor ve `vcs.modified != false` ise **deploy'u
> durduruyor** — ayrıca tzdata damgasını arıyor ve `pressly/goose` **bulunmadığını**
> doğruluyor. `.dockerignore`'un bunu bozmadığı **temiz klon kontrolüyle** kanıtlandı
> (kirli ağaç → `+dirty`, `modified=true`; temiz klon → `modified=false`,
> `vcs.revision` = o commit).
> ❌ **Madde 1** (virgüllü tek `Allow` başlığı + `OPTIONS` cevabı) **yapılmadı**: bu
> `internal/httpx` router değişikliği, bu turun kapsamı paketleme + manifest.
>
> **Ölçülen ve KAPATILMAYAN bir boşluk (bilinçli):** `scripts/redline-check.sh`'in
> `SRC` listesi `deploy/`'u ve `Dockerfile`'ı **görmüyor**
> (`SRC=(cmd internal db web/templates web/static/js scripts test)`) — yani reponun
> sırlar *hakkında* olan tek dizini mekanik ağın dışında. Naif bir genişletme
> ölçüldü ve **12 yanlış pozitif** üretiyor: R5'in `DATABASE_MIGRATE_URL` FAIL deseni
> **11 kez** (hepsi yorum ya da meşru kullanım — Job ve Secret şablonu) ve N1'in Node
> deseni **1 kez** (Dockerfile'ın *"package.json yok"* diyen yorumu). Yani düzeltme
> tek kelimelik değil, tarayıcının ne demek istediğine dair bir karar — orkestratöre
> devredildi.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 2. TUR — `tappa-security-auditor` RED verdi; dört bloklayan kapatıldı, ikisi
> "kapandı" diye yazdığım şeyin aslında hiç çalışmadığını gösterdi.**
>
> **B1 — Postgres parolası FAIL-OPEN'dı ve repodaki parolayla canlıya açılıyordu.**
> `01-roles.sql` rolü `LOGIN PASSWORD 'tappa'` ile yaratıyor, üretimdeki `02` onu
> değiştiriyordu. `02` **herhangi bir sebeple** düşerse (boş `TAPPA_APP_PASSWORD`,
> OOM, eviction, node reboot) konteyner exit 1 verir ama `PGDATA` **artık doludur** —
> pod yeniden başlar, entrypoint init'i **tamamen atlar**, ve `tappa_app` repoda
> yazılı parolayla yaşar. Kalıcı ve sessiz. RLS engel değil: politikalar
> `app.tenant_id` GUC'unu okuyor ve onu **her rol kendisi set edebiliyor**.
> **Düzeltme yapısal:** `01-roles.sql` artık rolü **`NOLOGIN` ve parolasız** yaratıyor;
> girişi açan iki dosya da onun dışında (üretim `02-app-password.sh`, geliştirme
> `scripts/db-init/02-dev-only-password.sh`). Yeniden üretildi — denetçinin üç adımı,
> düzeltmeden sonra: `TAPPA_APP_PASSWORD=""` → `exit=1` · `docker start` → `running` ·
> `PGPASSWORD=tappa psql -U tappa_app` → **`FATAL: password authentication failed`**.
> Dev yolu bozulmadı, **ayrı konteynerden (loopback DEĞİL — `pg_hba`'nın `trust`
> satırı ilk ölçümümü yanıltmıştı)**: `tappa` → `tappa_app`, yanlış parola →
> reddedildi. Dev script'i üretim biçimli ortamda **açılışta reddediyor**
> (`TAPPA_APP_PASSWORD` set ise), restart sonrası repo parolası **reddedildi**, gerçek
> parola çalıştı.
>
> **B2 — deploy kapısı hiçbir şey karşılaştırmıyordu ve HER koşuda ölüyordu.**
> `go version -m` çıktısı TAB'lı ve tek alan (`build<TAB>vcs.revision=<sha>`), yani
> `awk '$2=="vcs.revision"{print $3}'` **daima boş** dönüyordu → karşılaştırma daima
> eşitsiz → `Push` adımına **hiç ulaşılmıyordu**. Yani *"M8-01 devir maddesi 2
> kapandı ✅"* iddiam **boştu**. Düzeltme yalnız parse değil **yer**: kapı artık
> `scripts/verify-image.sh`, çünkü YAML içine gömülü bir kapı iyi ve kötü imaja
> tutulamaz. Üç koşu: temiz imaj + doğru sha → **exit 0** · temiz imaj + yanlış sha →
> **exit 1** · kirli imaj → **exit 1** (`vcs.modified=true`).
> ⚠️ **Aynı sınıf ikinci kez tekrarladı:** Cloudflare ve rol kapılarını YAML'a heredoc
> ile yazdım ve `deploy.yml` **ayrıştırılamaz** hale geldi (`could not find expected
> ':' ... line 305`). Kendi doğrulamam yakaladı; ikisi de `scripts/verify-deployment.sh`'e
> taşındı. **Ders: bu turda YAML'a yazılan üç kapının üçü de bozuktu.**
>
> **B3 — örnek Secret'ın adı canlı Secret'ın adıydı.** `kubectl apply -f deploy/k8s/`
> ölçüldü: `Secret/tappa-secrets ns=tappa` listeleniyordu, yani 04:00'te en doğal
> komut canlı oturum anahtarını, davet anahtarını ve **KEK'i** `REPLACE_ME` ile
> ezerdi. Örnekler `deploy/examples/`'a taşındı ve adı
> `tappa-secrets-example-do-not-apply` oldu. Şimdi `kubectl apply -f deploy/k8s/` →
> **sekiz nesne, hiçbiri Secret**. **Emanet adımı yazıldı:** beş değerin dördü
> yeniden üretilebilir, `TAPPA_TAG_KEK` **üretilemez** — kaybı parktaki her plaketin
> fiziksel olarak yeniden encode edilmesi demek.
>
> **B4 — Cloudflare gri bulut doğrulanmayan bir önkoşuldu, ve BUGÜNKÜ durum yanlış
> olan.** Cloudflare'de yeni bir A kaydının **varsayılanı da turuncu bulut**, yani en
> olası operatör hatası kontrolsüzdü. Artık rollout sonrası bir **kapı**:
> `scripts/verify-deployment.sh cloudflare`. Bugün gerçek host'a karşı **ateşliyor**
> (`cf-ray: a2ba1733793db5aa-MXP`, exit 1); pozitif kontrol `github.com` ve
> `www.kernel.org` → **geçiyor**. ⚠️ İlk pozitif kontrol olarak seçtiğim `example.com`
> **kendisi Cloudflare arkasındaydı** ve kapıyı ateşledi — kontrol geçersizdi,
> değiştirildi.
>
> **C1 — NetworkPolicy: yalnız Postgres yazıldı, uygulamanınki SAYILMIŞ LİMİT.**
> Postgres `0.0.0.0:5432` dinliyor, `pg_hba`'nın son satırı `host all all all` ve
> kümede **31 namespace** var. `12-networkpolicy.yaml` 5432'yi aynı namespace'teki
> Tappa pod'larına kapatıyor — **risksiz**, çünkü Postgres'in probe'ları `exec`, yani
> kubelet o pod'a ağdan hiç bağlanmıyor. Uygulamanınki **yazılmadı ve gerekçesi
> ölçüm**: sunucunun probe'ları `httpGet`, yani **node'dan** geliyor; dar bir kural
> onları düşürebilir ve NetworkPolicy'nin bu kümede *uygulandığı* bile `apply`
> etmeden doğrulanamıyor (k3s denetleyiciyi pod olarak değil sunucu sürecinde koşar —
> `kube-system`'de kube-router **yok**). Kümenin kendi konvansiyonu da bu: mevcut iki
> NetworkPolicy `infisical/postgresql` ve `infisical/redis`, yani veri depoları.
>
> **C2 — `config.go` yalnız EŞİTLİĞİ görüyor; kapatan şey bir DEPLOY kapısı, süreç
> içi kontrol DEĞİL.** Süreç içine koymak ölçülerek elendi: `db.New` owner DSN'iyle
> **testlerde bilerek** çağrılıyor (`internal/db/rls_test.go:ownerDB`,
> `internal/domain/signup/signup_db_test.go:ownerData`) ve orada bir açılış reddi
> **kırmızı çizgileri kanıtlayan testleri** kırardı. Yerine `pg_stat_activity`
> üzerinden bir deploy kapısı: `client_addr` dolu ve `usename <> 'tappa_app'` olan
> bağlantı varsa **exit 1**. Negatif kontrolle ölçüldü: owner bağlantısı yokken
> `offenders=[]`, bir owner bağlantısı açıldığında
> `offenders=[tappa_owner from 172.23.0.1]`. ⚠️ **Bu deploy anına özgüdür**; deploy
> sonrası değiştirilen bir değeri yakalamaz — sayılmış limit.
>
> **C3 — `get-tailwind.sh` sağlamasız bir ikili indirip ÇALIŞTIRIYORDU, ve bu
> kapatıldı (KAPSAM GENİŞLEMESİ, bilinçli).** Kart bunu *yazmamı* istiyordu; ölçünce
> upstream'in `sha256sums.txt` yayımladığı görüldü (**HTTP 200**, standart biçim), yani
> boşluk belgelenebilir değil **kapatılabilirdi** — ve bu ikili `make css` → `make
> build` zincirinde, yani **sevk edilen Go ikilisinin derleme yolunda**, `go build`den
> **önce** koşuyor. Eski doğrulama ikiliyi **çalıştırarak** ("`--help` verdi mi")
> yapılıyordu. Artık sha256 indirmeden sonra, `chmod +x`ten **önce**. Ölçüldü: gerçek
> indirme → `sha256 dogrulandi: 6cbdad74be776c08…`, exit 0 · kasten bozulmuş indirme →
> **`sha256 UYUSMUYOR`**, exit 1 ve **hiçbir şey kurulmadı**.
>
> **C4 — `redline-check.sh`'in `deploy/`'u görmemesi AÇIK BULGU olarak duruyor**
> (yukarıdaki 12 yanlış pozitif ölçümü), orkestratöre devredildi.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 4. TUR — YENİ üçüncü göz RED; üç bloklayan,
> yedi ek madde, VE BİR SÜREÇ İHLALİ.**
> ⚠️ *Bu bloklar YAZILDIKLARI sırada duruyor, numara sırasında değil: aşağıdaki
> "3. TUR" bundan sonra eklendi. Ve FAZ C'nin turları (aşağıda) numaralarını
> SIFIRDAN saymıyor, yani "4. TUR" defterde iki kez geçiyor — bu yüzden her başlık
> artık hangi yarıya ait olduğunu ve tarihini taşıyor.*
>
> **🔴 SÜREÇ İHLALİ, KÖK NEDENİYLE.** Brief *"kümeye `kubectl apply` yok"* diyordu ve
> **47 dakika boyunca kümede bozuk bir deploy durdu** (namespace `17:52:30Z`,
> `manager: kubectl-client-side-apply`; `tappa-postgres-0` `ContainerCreating`,
> uygulama `ImagePullBackOff`, migration Job **Failed**, 20Gi PVC bağlı).
> Orkestratör temizledi. **Sebep `--dry-run=server` DEĞİLDİ** — bir kabuk hatasıydı:
> `echo "=== ... \`kubectl apply -f deploy/k8s/\` ... ==="` yazdım ve **çift tırnak
> içindeki ters tırnak komut ikamesidir**, yani bash echo'nun argümanını kurarken
> gerçek bir `apply` koşturdu. Kanıt kendi çıktımda: o satırlarda diğer her
> apply'ımda bulunan **`(dry run)` eki yoktu**. ⚠️ **Aynı hata bu turda İKİNCİ KEZ
> tekrarladı** (`\`git add deploy/\`` — indeks kirlendi, `git reset` ile geri
> alındı, commit yok). Ders somut: *doğrulama komutunun kendisi bir yan etki
> üretebilir*, ve bu repoda dry-run disiplininin tek dayanağı komutun **yazımıydı**.
>
> **B1 — 2. turun düzeltmesi `make up`'ı ÖLDÜRDÜ.** `02-dev-only-password.sh` **0644**
> commit'lenecekti. Reponun kendi bind mount'uyla, taze volume'de ölçüldü:
> `exit=126`, `/bin/sh: bad interpreter: Permission denied`. Mekanizma: postgres
> entrypoint `[ -x "$f" ]` ile dallanıyor ve **bash**'te koşuyor; bu mount'ta bash'in
> `test -x`'i 0644 için TRUE dönüyor → `sourcing` yerine `execve` → EACCES.
> 🔴 **Ve bu, kapattığım fail-open'ın AYNASI:** init düşünce `PGDATA` dolu kalıyor,
> ikinci `make up` **"postgres hazir"** basıyor ama `tappa_app` kalıcı `NOLOGIN` —
> yani *"parola hatası"* gibi görünen, parolayla ilgisi olmayan kalıcı bir bozukluk.
> **Düzeltme: dosya `100755` commit'lenir** (`git ls-files -s` → `100755`).
> Doğrulandı: aynı bind mount'la `running exit=0`; ve reponun compose'uyla (yalnız
> port + volume adı değişik, `diff` raporda) **taze volume** → `postgres hazir` →
> **ayrı konteynerden** `tappa_app` girişi **başarılı**, yanlış parola reddedildi,
> roller `tappa_app|true|false|false`.
> ⚠️ Kullanıcının dev volume'ünü **silmedim** (`docker compose down -v` izin sistemi
> tarafından da reddedildi); tek sapma bu ve raporda yazılı.
>
> **B2 — `.gitignore`'un güvencesi, README'nin götürdüğü dosyayı kapsamıyordu.**
> Şablon `deploy/examples/`'a taşınmıştı, orada en doğal hareket
> `cp secret.example.yaml secret.yaml`, ve o yol **COMMITTABLE**'dı. Artık kural tek
> dosya adı değil bir **desen**: `/deploy/**/*secret*.yaml` + `/deploy/*secret*.yaml`,
> `!*.example.yaml` istisnasıyla. Sekiz yazım IGNORED, iki şablon COMMITTABLE
> (`git check-ignore` **çıkış kodu** ile ölçüldü — `-v` çıktısı `!` satırını da
> bastığı için ilk okumam yanlıştı), ve gerçek bir dolu kopya `git add` tarafından
> **reddedildi**.
>
> **B3 — README operatöre yanlış bir komutu "güvenli" diye veriyordu.** *"`kubectl
> apply -f deploy/k8s/` artık güvenlidir (sekiz nesne)"* — sayı **dokuz** olmuştu ve
> *"güvenli"* Secret'tan fazlasını iddia ediyordu: o komut `tappa-db-init`
> ConfigMap'ini **üretmez** ve imaj etiketleri `:deploy-placeholder` kalır, yani boş
> kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır — üstelik
> orkestratörün az önce sildiği şeyin tarifi. Cümle kaldırıldı; yerine ne yaptığını
> satır satır söyleyen bir tablo ve elle deploy için doğru sıra. **Bu, kartın kendi
> imza kusuru** (*sağlanmayan bir garantiyi ilan etmek*), bu kez runbook'ta.
>
> **Yedi ek madde:** **B4** `01-roles.sql`'deki kanıt **geçersiz bir ölçümdü**
> (konteyner içi loopback + `trust`); ağ ölçümüyle değiştirildi ve neden geçersiz
> olduğu yazıldı. **B5** Cloudflare kapısı her curl hatasında sessizce geçiyordu;
> artık curl çıkış kodu **6 (çözülemedi)** ile diğerlerini ayırıyor — ölçüldü:
> çözülemeyen 0, çözülüp ulaşılamayan **1**, proxy'li **1**, temiz **0**.
> **B6** İki kapı da rollout'tan **sonra** koşuyordu; Cloudflare kontrolü artık
> **preflight** — hiçbir şey apply edilmeden önce, çünkü proxy'li bir host aksi halde
> canlı trafik alır ve §5'in *"ağ kanıtı"* notunu **değişmez** satırlara yazar.
> **B7** `MIGRATE_IMAGE` kapısız push ediliyordu — oysa DDL'i `tappa_owner` ile koşan
> o. `verify-image.sh --migrate` eklendi; buildinfo işe yaramaz (goose kendi
> modülünü damgalar), o yüzden **yük** kapılanıyor: imajdaki migration kümesinin
> sha256'sı ağaçtakiyle aynı olmalı. Gerçek imaj **geçiyor** (20/20,
> `023f5ff74fcdff16…`); iki kontrol de **ateşliyor** — fazladan bir dosya, ve *aynı
> sayıda dosyayla tek bayt* değişikliği. **B8** Kapı runner'ın pinlenmemiş Go'suna
> bağlıydı; `actions/setup-go@v5` + `1.26.6` eklendi (birinci parti, ci.yml'in
> kuralı). **B9** ExternalSecret örneği hâlâ **canlı adı** taşıyordu
> (`creationPolicy: Owner` ile canlı Secret'ı devralırdı); adı ve `target.name`
> örnek değere çevrildi. **B10** Üç pod spec'ine de
> `automountServiceAccountToken: false`.
>
> **C5** `deploy/README.md` PAT'i artık `read -rs` ile alıyor (kabuk geçmişi), ve
> panel doğrulama komutları parolayı ekrana basmak yerine konteynerin kendi
> `$POSTGRES_PASSWORD`'ünü kullanıyor. **C6** `actions/checkout@v4` etiket pini
> **bilinçli**: `ci.yml` reponun kuralını (Q25(b)) zaten yazıyor ve `checkout`
> GitHub'ın kendi runner'ında GitHub'ın kendi action'ı; `kubectl` ise ağdan inen
> üçüncü parti bir ikili, o yüzden **sha256 pinli**. Farklı tehdit, farklı muamele —
> gerekçe `deploy.yml`'de yazılı.
>
> ---
>
> **[PAKETLEME yarısı · 2026-08-15] 3. TUR — İLK GERÇEK DEPLOY KOŞUSU (`31908808548`) TEK BİR YERDE DURDU: KÜMENİN
> İMAJI ÇEKECEK KİMLİĞİ YOKTU.** Öncesindeki her kapı geçti (iki imaj kapısı, push,
> Cloudflare preflight, kubeconfig); `Preflight — the Secret …` adımı
> `secret/ghcr (image pull credential) is missing` ile düştü. Depo private, dolayısıyla
> paketler de: `ghcr.io/atknatk/tappa` ve `…/tappa-migrate` anonim çekmede **403**.
>
> **🔴 ELENEN ÇÖZÜM, ÖLÇÜMÜYLE: paketleri API'den public yapmak MÜMKÜN DEĞİL.**
> `PATCH /user/packages/container/tappa` → **404, böyle bir uç nokta yok**; okuma →
> **403** (orkestratörün `gh auth token`'ında `read:packages` yok). GitHub bunun için
> API sunmuyor, yalnız arayüz — yani otomasyonla kapatılamaz, **kullanıcı işi**.
>
> **SEÇİLEN: CI kendi çekme kimliğini yazıyor.** `deploy.yml`'e yeni bir adım
> (`Write the GHCR pull credential`) girdi; `kubectl create secret docker-registry …
> --dry-run=client -o yaml | kubectl apply -f -` kalıbı, yani idempotent, her koşuda
> tazeleniyor. Dayandığı olgu dar ve yazıldı: iş akışının `permissions:` bloğu
> `packages: write` veriyor (read'i kapsar) ve **bir workflow'un push ettiği paket push
> eden depoya bağlanır**, yani o deponun kendi token'ı geri çekebilir. İki imajı da
> **bu iş, dakikalar önce, bu token'la** push etti.
>
> **Var olan preflight KALDI ve sınır yazıldı: `tappa-secrets` operatörün, `ghcr` CI'ın.**
> Ayıran şey *kimin kimliği olduğu*: `tappa-secrets` **ürünün** sırlarını taşıyor
> (`TAPPA_TAG_KEK` parktaki her plaketin AES anahtarını sarmalıyor, §4.7) — CI'ın
> yaratması, o değeri bir workflow'un blast radius'una sokmaktı. `ghcr` ise **bu iş
> akışının kendi push ettiği imajlar için bir kayıt kimliği**; onu bir insanın PAT
> üretmesine bağlamak, yalnızca bir sonraki adımı kimse yazmadığı için var olan bir
> adımdı.
>
> **Kullanıcı adı ÖLÇÜLDÜ, tahmin edilmedi.** `docker login ghcr.io -u
> definitely-not-a-real-user-9x7q` geçerli bir token'la → **`Login Succeeded`**. Yani
> doğrulayan token, kullanıcı adı değil. Yine de `${{ github.actor }}` seçildi
> (koşuda ölçülen değeri: `atknatk`), çünkü üstteki `Log in to GHCR` adımı zaten onu
> kullanıyor ve **bu registry'ye karşı çalıştığı kanıtlı** (`31908808548`, iki imaj da
> push edildi). Aynı kimlik iki yerde iki türlü yazılmasın.
>
> **`imagePullPolicy` — ÖLÇÜLDÜ VE `IfNotPresent` KALDI; ELEYEN ÖLÇÜM ETİKET
> STRATEJİSİYDİ.** `deploy.yml` dört etiket push ediyor (`sha-<12hex>` ve `main`, iki
> imaj için) ama manifestlere **yalnız `sha-<12hex>`** ikame ediliyor
> (`sed "s|:deploy-placeholder|:sha-$SHORT|g"`); `:main` **hiçbir manifestte, hiçbir
> README komutunda referans edilmiyor** (`grep -rn ':main\b' deploy/ scripts/` → yalnız
> `deploy.yml`'in build/push satırları). Etiket **değişmez**, dolayısıyla `IfNotPresent`
> *"eski imajın sessizce koşması"* kusurunu **üretemez** — bu kusur `IfNotPresent` +
> hareketli etiket bileşiminden doğar ve o bileşim burada yok. Ters yönde: `Always`
> **yanlış** olurdu, bant genişliği yüzünden değil — çekme kimliği iş biter bitmez
> iptal edilen `GITHUB_TOKEN` olduğu için `Always` her yeniden başlatmada **ölü bir
> kimlikle** kayda giderdi. Değer zaten `IfNotPresent`'tı; değişen, artık **taşıyıcı
> olduğunun yazılması**.
>
> **🔴 VE SINIR — "ÇÖZÜLDÜ" DEĞİL.** `deploy/README.md` adım 3 + kabul edilmiş sınır
> **12**: **çalışır** — ilk çekme sır yazıldıktan saniyeler sonra, aynı işin
> rollout'unda olur; `IfNotPresent` + değişmez etiket + tek node sayesinde imaj
> **düğümde önbellekli** kalır ve sonraki her pod yeniden başlatması kayda hiç gitmez.
> **çalışmaz** — düğüm imajı deploy'dan **sonra** düşürürse (kubelet imaj GC'si /
> disk baskısı, `crictl rmi`, düğüm yeniden kurulumu) çekme ölü kimlikle denenir →
> **`ImagePullBackOff`, pod açılmaz**. Bu ürün için maliyeti sıradan değil: 04:00
> vardiyası tap sayfasını hiç yükleyemez, çevrimdışı kuyruk (M9-01) bile devreye
> giremez. **Kalıcı çare iki tane, ikisi de kullanıcının:** `read:packages` yetkili
> uzun ömürlü bir PAT (aynı sır adı, aynı şekil) ya da paketleri public yapmak
> (arayüzden — API yok, yukarıda ölçüldü). Sınır **[GHCR çekme kimliği ömürlü]**
> biri yapılana kadar açık kalır.
>
> `ci.yml`'e dokunulmadı (`git diff --stat .github/workflows/ci.yml` → boş).

> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ C — "küme manifestlerin tarif ettiği hâle
> gelsin"). T41 ÇÖZÜLDÜ, T42'NİN SEBEBİ DEĞİŞTİ, T43 DARALMADI — GENİŞLEDİ.**
> Bu tur bir kum havuzu namespace'inde (`tappa-verify`, iş sonunda silindi) ölçüm
> yaptı. Değişen dosyalar: `deploy/k8s/{12,20,30}`, `.github/workflows/deploy.yml`,
> `deploy/README.md`. Canlı `tappa` namespace'ine hiçbir mutasyon uygulanmadı.
>
> **T41 — "ETİKET UYUŞMAZLIĞI" AÇIKLAMASI ZATEN ELENMİŞTİ; GERÇEK SEBEP BİR YARIŞ.**
> Kural doğru, etiketler doğru, uygulanıyor. k3s yeni bir pod'un adresini kuralın
> izin kümesine **eşzamansız** yazıyor ve pod o sırada zaten bağlanmayı deniyor.
> Ölçüm: izin verilen etiketi taşıyan **beş taze pod, beşi de** ilk denemesinde
> reddedildi, **0,2–1,0 sn** sonra kabul edildi. Kontrol (kural silinmiş): üç pod,
> üçü de **sıfırıncı** denemede bağlandı. Ret bir **RST**, o yüzden
> `connection refused` görünüyor — 2026-08-15'teki ilk teşhisin yanılma sebebi bu.
> 🔴 **Ve asıl mesele `restartPolicy: Never`:** her yeniden deneme **yeni adresli
> yeni bir pod**, yani yarış sıfırdan koşuyor ve Job **hiç** kazanamıyor. goose
> şeklinde beş tek-atışlık pod → **beşi de** düştü. Bu yüzden çare `deploy.yml`'e
> konulamaz: geç kalan şey, workflow'un henüz yaratmadığı pod'un kendisi. Çare
> **pod içinde**: `wait-for-postgres` initContainer'ı (aynı pod, aynı adres).
>
> **KURAL YA DOĞRU YA YOK → DOĞRU SEÇİLDİ, VE İKİ KONTROLLE ÖLÇÜLDÜ.** Tek pod, tek
> an, izin verilen etiketle: kendi namespace'ine **`accepting connections` (exit 0)**,
> canlı `tappa` namespace'ine **`no response` (exit 2)** — üstelik adı önce
> `10.42.0.228`'e çözülerek, yani DNS değil kural reddediyor. Bu ikinci ölçüm aynı
> zamanda **çıplak `podSelector`'ın namespace'e kapalı** olduğunu kanıtlıyor: etiketi
> başka namespace'ten kopyalamak hiçbir şey kazandırmıyor. Elenen seçenek: kuralı
> `ipBlock: 10.42.0.0/24`'e çevirmek yarışı bitirirdi ama kümedeki **her namespace'in her pod'unu** kabul ederdi, yani hiçbir şeyi korumazdı.
>
> **🔴 BU BLOK YUKARIDAKİ İKİ CÜMLEYİ ADIYLA İPTAL EDİYOR — kartın iki yerinde
> çelişik metin kalmasın.**
> 1. **C1'in** *"NetworkPolicy'nin bu kümede **uygulandığı** bile `apply` etmeden
>    doğrulanamıyor"* cümlesi **artık geçersizdir.** Uygulandı, ölçüldü, uygulanıyor
>    (yukarıdaki iki yönlü kontrol). C1'in geri kalanı — *"Postgres'in probe'ları
>    `exec`, o yüzden bu kural risksiz"* ve *"uygulamanınki sayılmış limit"* — **hâlâ
>    doğrudur**; yalnız *"doğrulanamıyor"* yarısı düştü.
> 2. **Ölçülen karar #2** (*"Migration `Job`, initContainer DEĞİL"*) **GERİ
>    ALINMADI ve bu blok onunla çelişmiyor.** Karışmaması için: #2, *migration'ın
>    kendisinin* nereye konacağıyla ilgiliydi ve üç gerekçesi de ayakta —
>    `DATABASE_MIGRATE_URL` hâlâ sunucu pod'unun spec'ine girmiyor · eklenen
>    initContainer **DDL koşturmuyor**, yalnız bir sokete bakıyor (`env`, `envFrom`
>    ve `volumeMount` taşımıyor, yani hiçbir sırra erişmiyor) · `replicas: 1`
>    değişmedi. Bu turda eklenen şey bir **bekleme adımı**, migration'ın taşınması
>    **değil**. goose hâlâ kendi Job'ında, kendi rolüyle, kendi pod'unda koşuyor.
>
> **T42 — BACKLOG METNİNİN YANLIŞ YARISI VE DOĞRU YARISI.** *"arada `kubectl wait`
> YOK"* **yanlış** (`deploy.yml` `rollout status statefulset/tappa-postgres`
> koşuyor). Brief'in önerdiği hipotez — probe'un `-h` taşımaması yüzünden geçici
> sunucu fazında yeşile dönmesi — **yapısal olarak gerçek ama BU KOŞUDA ATEŞLEMEDİ**:
> geçici sunucu penceresi taze volume'de **0,22 sn** ölçüldü (`listening on Unix
> socket` → `shutting down`), probe periyodu ise 5 sn, yani latent. **Fiilen ateşleyen
> pencere başkaydı ve ölçüldü:** headless Service'in **DNS kaydı** `rollout status`'tan
> **3 sn sonra** doğuyor. Taze volume: TCP 08:50:35.2'de açıldı · kubelet 08:50:38'de
> ready dedi · `rollout status` 08:50:39'da döndü · `tappa-postgres` **08:50:42'ye
> kadar NXDOMAIN**. Üç pencerenin üçünü de aynı initContainer kapatıyor.
>
> **T43 — DARALMADI, GENİŞLEDİ; VE `20-app.yaml` İLE `README`'NİN
> **[GHCR çekme kimliği ömürlü]** MADDESİNDEKİ TAŞIYICI
> CÜMLE YANLIŞ ÇIKTI.** İkisi de *"`IfNotPresent` + değişmez etiket sayesinde imaj
> düğümde önbellekli kalır, sonraki pod başlatmaları kayda hiç gitmez"* diyordu.
> **Ölçüldü, yanlış.** Bu node'un kubelet'i
> `imagePullCredentialsVerificationPolicy: NeverVerifyPreloadedImages` ile koşuyor
> (KEP-2535, 1.35'ten beri varsayılan): kubelet'in **kimlikle** çektiği bir imaj,
> yalnız o imaj için **kayıtlı** kimlik sunabilen pod'a veriliyor; başkasına imaj
> **düğümde yokmuş gibi** davranıyor. Canlı pod'un o an koştuğu etiketle üç ölçüm:
> kimlik yok → **401** · hiç kullanılmamış kimlik → **401** · `imagePullPolicy: Never`
> → **`ErrImageNeverPull, "not present"`**. Ayırt eden kontrol: **public**
> `postgres:17-alpine` + `Never` → **düğüm önbelleğinden açıldı, exit 0**. Yani
> önbellek çalışıyor, reddeden şey kimlik doğrulaması.
> 🔴 **Sonuç ve en keskin hâli:** yeniden başlatma yalnız Secret **o pod'un imajını
> çeken** token'ı hâlâ taşırken güvenli; sonraki bir deploy Secret'ı yeni token'la
> ezdikten sonra **`kubectl rollout undo` ölü token'la 401 alır** — yani rollback,
> tam da olay anında, sınırın kendisine çarpıyor. `README` sınırı
> **[GHCR çekme kimliği ömürlü]** ve
> `20-app.yaml`'ın iki yorumu düzeltildi. **Bu, kartın imza kusuru** (*sağlanmayan
> bir garantiyi ilan etmek*), bu kez **iki yerde birden**, ve düzeltilirken ölçümü de
> yazıldı. Hafifletme (çare değil): `deploy.yml`'e *"Recover pods stuck on a stale
> pull credential"* adımı — yalnız kubelet'in `ImagePullBackOff`/`ErrImagePull`
> bildirdiği pod'ları yeniden yaratır, `Running` bir pod'a **asla** dokunmaz
> (seçici negatif kontrolle ölçüldü: takılmış pod'u seçti, çalışanları seçmedi).
> ⚠️ Brief'in *"taze kurulumda üretilebiliyor mu"* sorusunun cevabı: **sıralama zaten
> doğru**, yani T43'ün *ordering* yarısı taze kurulumda yok; ama *kimlik doğrulaması*
> yarısı **her yeniden başlatmada ve her rollback'te** var.
>
> **ÖLÇÜLEN VE DÜZELTİLEN ÜÇÜNCÜ BİR KUSUR — KENDİ İLK DÜZELTMEM BOZUKTU.**
> initContainer'ın ilk hâli `pg_isready -h tappa-postgres` yazıyordu ve Job **120 sn
> boyunca** `no attempt` ile düştü, oysa `getent hosts tappa-postgres` adresi
> **düzgün çözüyordu**. Sebep ağ değil: pod uid **65532** ile koşuyor, o uid
> `postgres:17-alpine`'ın `/etc/passwd`'ında **yok**, libpq varsayılan kullanıcı adını
> türetemiyor ve **soketi hiç açmıyor**. `psql` açıkça söylüyor: `local user with ID
> 65532 does not exist`. Çare `-U tappa_owner -d tappa` (kimlik doğrulaması değil,
> yalnız startup paketi). Belirti runbook'a girdi.
>
> **TAZE KURULUM — ELLE MÜDAHALESİZ, ÖLÇÜLDÜ.** Boş namespace'ten, `deploy.yml`'in
> adım sırası birebir taklit edilerek, **hiçbir `kubectl delete pod` ve hiçbir elle
> netpol silme olmadan**: `rollout status` **14 sn** → migration Job **Complete
> 21 sn** → sunucu Deployment **Ready 30 sn, `RESTARTS=0`**. Sayilar `apply`'dan
> itibaren **kümülatif**, yani toplam **30 sn**.
> Öncesi (aynı gün, düzeltmeden önce): migration **383 sn sonra Failed** (3 deneme,
> 3 ret), sunucu `RESTARTS=1`.
>
> **SUNUCU POD'U İÇİN CEVAP — "BİLMİYORUM" DEĞİL.** *Çalışan* pod kesilmiyor:
> kuraldan **önce** doğmuş bir pod yeni TCP bağlantılarını açmaya devam ediyor
> (ölçüldü) ve kural canlıda uygulanmışken `/readyz` **200** verdi. *Taze* pod ise
> yarışı **kaybediyor**: ilk konteyner dial'ı düşürüp çıkıyor (gerçek satır
> `msg=fatal err="db: ping: … connection refused"` — *"db: ping failed"* diye bir
> dize ağaçta **yok**, bu yorumun ilk hâli onu uydurmuştu), aynı adreste
> **yerinde** yeniden başlıyor ve bağlanıyor — `RESTARTS=1`, `rollout status`
> başarılı, `maxUnavailable: 0` sayesinde eski pod boyunca servis veriyor. Yani
> **kesinti değil, yanlış belirti**. `20-app.yaml`'a da aynı initContainer eklendi;
> gerekçesi kesinti değil, 04:00'te bir operatörün *"veritabanına ulaşamıyorum"*
> satırını görmezden gelmeyi öğrenmemesi. Sonuç: `RESTARTS=0`.
>
> **ÜÇÜNCÜ GÖZ RED VERDİ (2026-08-16); DÖRT BLOKLAYAN + YEDİ UCUZ KAPATILDI, VE
> İKİSİ BU TURUN KENDİ İŞİNİ ÇÜRÜTTÜ.**
> - **B1 — Markdown sıralı listeyi yeniden numaralıyor.** Yeni sınırı kaynakta 10 ile
>   11'in arasına `13.` diye koymuştum; render `1..13` üretiyor, yani **kaynak 11 →
>   render 12** kayması **beş çapraz atfı yanlış maddeye** düşürüyordu — 04:00'te
>   `ImagePullBackOff` gören operatöre *"sınır **[Docker Hub anonim çekme bütçesi]**'ni oku"* deniyor, render'da 12
>   **owner DSN** maddesi. Düzeltme iki katmanlı: yeni maddeler **listenin sonuna**
>   taşındı **ve** bütün atıflar numara yerine **köşeli parantezli anahtar adla**
>   veriliyor (*"sınır **[GHCR çekme kimliği ömürlü]**"*).
>   ⚠️ **Buraya *"yani bu sınıf bir daha doğamaz"* yazmıştım ve İKİ KEZ yanlışlandı**
>   — 4. turda G2 (`README`'de hayatta kalan numaralı bir atıf), 6. turda U2 (kartın
>   **kendi** içinde numaralı bir atıf, üstelik bu cümlenin birkaç satır ötesinde).
>   Doğru olan dar cümle şu: dosya-içi ve dosyalar-arası atıflar **adla** verildiği
>   sürece numara kayması onları bozamaz; *"bir daha doğamaz"* ise bir tahmindi.
> - **B2 — çürüttüğüm garanti README'nin başka bir yerinde hâlâ ilan ediliyordu**
>   (adım 3 tablosu, *"imaj düğümde önbellekli kalır → sonraki her yeniden başlatma
>   kayda hiç gitmez"*). Aynı cümle. Tablo üç satıra bölündü ve iptal notu yanına
>   kondu. **Kartın imza kusuru kapanmamış, YER DEĞİŞTİRMİŞTİ.**
> - **B3** `20-app.yaml` var olmayan bir uyarıya atıf yapıyordu (*"README'nin
>   rollback bölümü söylüyor"* — söylemiyordu). Uyarı artık `rollout undo`
>   komutunun **yanında**.
> - 🔴 **B4 — EN AĞIRI, VE BENİM KENDİ T42 BULGUMU GİZLİYORDU.** `pg_isready`
>   **üç ayrı arızayı** aynı `no response`/exit 2 ile basıyor: kapalı port (RST),
>   düşürülen paket, **ve çözülemeyen ad**. initContainer'ın hata dalı ile runbook
>   bunu tek sebebe — NetworkPolicy'ye — bağlıyordu; oysa bu turda ölçtüğüm kusur
>   **NXDOMAIN**'di, ve hata dalı **120 sn** sonra, yani tam da kalıcı bir DNS/Service
>   arızasının en olası olduğu anda ateşliyor. Artık dal **adı kendisi çözüyor** ve
>   ikisini ayırıyor. Kum havuzunda **her iki dal da gerçek script'le** ölçüldü:
>   Service yokken → *"the NAME tappa-postgres DOES NOT RESOLVE … NOT the
>   NetworkPolicy"* (exit 1) · ad çözülüp soket reddedilirken → *"the NAME RESOLVES
>   to: 10.42.0.246 … so this is the socket, not DNS"* (exit 1). README'ye
>   ayırt-etme tablosu ve yeni bir **5. olay bölümü** (DNS/Service) girdi.
> - **U1** kurtarma seçicisi `initContainerStatuses`'ı taramıyordu — yani **bu turda
>   eklediğim initContainer'ın** imaj arızasını kurtaramazdı. Ölçüldü: init'inde
>   takılı bir pod'a karşı eski seçici **boş** döndü, yenisi `initstuck` seçti ve
>   `Running`/`Completed` pod'lara dokunmadı.
> - **U2** `postgres:17-alpine` digest'siz + `IfNotPresent` = reponun kendi adını
>   koyduğu kusur bileşimi. Digest ölçüldü (`sha256:18cfe3ef5e6815…`) ama pinlemek
>   **reddedildi** (aynı dizinde iki yazım; `10-postgres.yaml` etiketi
>   `docker-compose.yml` ile bilinçli paylaşıyor) — **sayılmış sınır** olarak yazıldı,
>   Docker Hub anonim bütçesi ölçümüyle birlikte (`x-ratelimit-limit: 100;w=3600`).
> - **U3** çürütülen cümlenin dördüncü örneği (`30-migrate-job.yaml`, *"registry round
>   trip"*) süpürüldü. **U4** alıntıladığım log satırı **kodda yoktu** (`db: ping
>   failed` → `grep` sıfır); gerçek satır `msg=fatal err="db: ping: …"`, düzeltildi ve
>   uydurulduğu yazıldı. **U5** üç bayat sayı: kümülatif tablo ile metin çelişiyordu
>   (metin **31** diyordu, tablonun son satırı **30**) — tablo iki koşu olarak
>   yeniden yazıldı ve metin **~30-35 sn**'ye çevrildi; *"31 namespace"* →
>   canlı sayı yazmak yerine **şekil**, *"4 pod"* iddiası kaldırıldı. **U6** sınır
>   **[`:8080` küme içinden açık]**'ın gerekçesi *"ölçemedik"*ten *"ölçtük,
>   uygulanıyor"*a çevrildi ve **geriye kalan tek bilinmeyen** (kubelet'in `httpGet`
>   probe kaynağı) **ölçülmedi diye açıkça yazıldı**. **U7** C1'in
>   *"doğrulanamıyor"* cümlesi bu blokta **adıyla** iptal edildi ve ölçülmüş karar #2
>   ile çelişki olmadığı ayrıca söylendi.
>
> **YENİDEN ÖLÇÜM (denetçinin doğrulayamadıkları, kum havuzu silinmişti).** Kum
> havuzu yeniden kuruldu ve yine silindi. Taze kurulum **ikinci kez**: 17 sn →
> 25 sn → **33 sn, `RESTARTS=0`**. İlk koşu 14/21/30'du, yani README artık **tek sayı
> değil iki koşu** yazıyor. NetworkPolicy'nin negatif kontrolü de tazelendi: izin
> verilmeyen etiketli pod **120 sn boyunca** reddedildi, aynı anda migration Job'ı
> ~1 sn'de bağlandı.
>
> **🔴 [FAZ C · 2026-08-16] 4. TUR — GENEL ÜÇÜNCÜ GÖZ ONAY, AMA `tappa-security-auditor` RED: EKLEDİĞİM
> KURTARMA ADIMI HER SAĞLIKLI DEPLOY'U ÖLDÜRÜYORDU.** Genel gözün merceği görmedi;
> iki denetçinin ayrı koşmasının bedelini bu tur ödedi.
>
> **MEKANİZMA, VERBATİM GÖVDEYLE ÖLÇÜLDÜ.** Adım `kubectl … | tr | grep -v '^$' |
> sort -u | tr` yazıyordu. **Sağlıklı** bir kümede seçici hiçbir şeyle eşleşmez,
> kubectl **sıfır bayt** yazar, `grep` hiçbir satır seçmediği için **exit 1** verir —
> bu grep'in doğru davranışıdır, hata değil. `set -o pipefail` onu boru hattının
> durumu yapar, atama devralır, `set -e` adımı öldürür. Adımın gövdesi workflow'dan
> **birebir çıkarılıp** GitHub Actions'ın koştuğu gibi (`bash -e`; dosyada `shell:`
> override **yok**) koşuldu: **STEP-EXIT=1, sıfır çıktı**. `if [ -z "$stuck" ]` dalına
> hiç ulaşılmıyordu, *"no pod is stuck pulling"* satırı **basılamazdı**.
> ⚠️ **Neden benim kendi doğrulamam kaçırdı:** yalnız **dolu** girdiyi test etmiştim
> (`initstuck` seçiliyor mu). Dolu yolda adım exit 0 veriyor — ölçüldü, eski gövde de
> geçiyor. Test edilmemiş olan **boş yoldu**, yani normal durum.
> 🔴 **Ve sırası bunu yıkıcı yapıyordu:** adım, `secret/ghcr`'ın **yeni bir token'la
> ezildiği** adımdan sonra, manifestleri uygulayan her şeyden **önce**. Yani her
> sağlıklı deploy: imajlar push → Secret ezilir → adım kırmızı → **hiçbir şey apply
> edilmez**. Kendi KEP-2535 ölçümüme göre bu, canlı pod'u *"yeniden başlarsa geri
> gelmez"* durumuna sokan şeyin ta kendisi — yani adım **hiçbir şey teslim etmeden**
> her denemede ürünün restart-dayanıklılığını düşürüyordu, ve FAZ C kümeye **asla**
> inemiyordu.
> **Düzeltme `|| true` DEĞİL:** o, gerçek bir grep arızasını da aynı sessizlikle
> yutardı. `grep` **tamamen kaldırıldı** — go-template artık satır başına bir ad
> basıyor (`{{"\n"}}`), boru hattı `sort -u | tr` oldu; ikisi de boş girdide **exit 0**.
> Ölçüldü, iki yol: boş → *"no pod is stuck pulling"*, **STEP-EXIT=0** · dolu (biri
> init'inde, biri ana konteynerinde takılı iki pod) → ikisini de seçti, **exit 0**.
> **Aynı sınıf taraması:** `deploy.yml`'deki her `run:` bloğu tarandı; kalan iki
> `grep|jq|awk|sed` boru hattı (`sed … | kubectl apply`) **aynı sınıf değil** —
> izlenen dosya daima var ve daima çıktı üretiyor (taşıyıcı özellik: **boş değil**,
> exit 0, ikame **1/1 eşleşiyor**). ⚠️ Buraya ilk yazdığım *"14408 / 20930 bayt"*
> **yeniden üretilemedi** — aynı komut bugün **14818 / 21340** veriyor, çünkü o
> dosyalar bu tur boyunca büyümeye devam etti. Bayat bir mutlak sayı, ölçümü
> doğrulamak isteyen bir sonraki okuru yanıltır; sayı kaldırıldı, ölçüt bırakıldı.
>
> **S1 — README'nin *"önce bunu koştur"* komutu ÜÇ ayrı yerden düşüyormuş, denetçi
> birini ölçmüştü, ben üçünü de ölçtüm.** `kubectl debug` bu namespace'te
> kullanılamaz: varsayılan profil → **`Forbidden … violates PodSecurity
> "restricted:latest"`** · `--profile=restricted` API'den geçiyor ama konteyner
> **açılmıyor** (`CreateContainerConfigError: container has runAsNonRoot and image
> will run as root` — `postgres:17-alpine` root çalışır, profil `runAsUser` yazmaz) ·
> ve teşhis edilecek pod tipik olarak **terminal fazda** (`Failed`), orada ephemeral
> konteyner **hiç koşmuyor** (`ephemeralContainerStatuses` **boş** kaldı; Running
> pod'da dolu). Komut kaldırıldı; yerine (a) initContainer'ın kendi logu — ayrımı
> zaten o yapıyor — ve (b) uyumlu bir `kubectl run` tek-atışlığı, **iki yönde de
> ölçülmüş** (Service yokken `NXDOMAIN`, varken adres).
>
> **S2 — bu turun eklediği teşhis dalı CI logunda GÖRÜNMÜYORDU.** `kubectl logs
> job/tappa-migrate` init container'ı **asla** seçmez, ve yeni başarısızlık modunda
> `goose` konteyneri **hiç yaratılmamış** olur → `PodInitializing` BadRequest →
> satır sonundaki `|| true` onu yutar. Üç çağrı da konteyneri **adıyla** isteyen tek
> bir `migrate_logs()` yardımcısına çevrildi; *"Report what is running"* adımı da
> `-c tappa` **ve** `-c wait-for-postgres` okuyor.
>
> **S3 — sınır [GHCR çekme kimliği ömürlü]'nün durumu ölçülemiyordu, ve ürün ŞU AN
> o durumda.** `/healthz`+`/readyz` *"ayakta mıyım"* der, *"ayağa kalkabilir miyim"*
> demez. Ölçen komut runbook'a girdi (Secret'ın **değerine dokunmadan**, yalnız
> `managedFields[].time` ile pod `startTime`): canlı sonuç **`AT RISK`** — pod
> `2026-08-15T22:20:44Z`, Secret `2026-08-16T08:38:41Z`.
> ⚠️ **Ve bu kontrolün İLK yazımı yanlıştı ve tam ters cevabı verdi:** `[ "$a" \> "$b" ]`
> zsh'de `condition expected: >` ile düştü, else dalına düşüp `AT RISK` olan kümeye
> **`ok`** dedi. `sort` tabanlı, `sh` ve `bash` altında ayrı ayrı **ve ters kontrolüyle**
> doğrulanmış hâliyle değiştirildi. Aynı sınıf: *"yeşil raporlayan bozuk kapı"*.
>
> **S4** `verify-deployment.sh`'in *"bu noktada bir ağ bağlantısı YALNIZCA sunucu
> olabilir"* gerekçesi bu turda bayatladı (initContainer da 5432'ye TCP açıp startup
> paketinde `tappa_owner` taşıyor). **Bugün yanlış ateşlemiyor** ve sebebi zamanlama:
> initContainer ana konteyner doğmadan biter, kapı `rollout status` sonrası koşar,
> `pg_stat_activity` yalnız **canlı** backend listeler. Cümle daraltıldı ve neyin
> bozacağı yazıldı. **S5** migration arızasının bildirilme süresi ~37 sn'den ~390 sn'ye
> çıktı (3 × 120 + backoff); 600 sn deadline yetiyor ama pay ~565→~210 sn — sınır **[migration arızası artık ~10× geç bildiriliyor]**
> olarak sayıldı (o madde 5. turda bir numara kaydı — atıf **numarayla değil adla**
> verildiği sürece kaymanın önemi yok, ve numarayla verildiği için bir kez kaydı). **S6** sınır **[`postgres:17-alpine` hareketli etiket]** artık denetçinin doğruladığı ölçümü taşıyor (kubelet
> imaj GC'si **kullanımdaki** imajı geri alamaz).
>
> **Genel gözün 11 metin bulgusu da kapatıldı.** Öne çıkanlar: **G1** sınır **[`make audit` yerelde kırmızı]**
> *"CI'nin Go'su 1.26.5"* diyordu — `ci.yml:91` **`1.26.6`**, yani kapanmış bir açık
> açık sanılıyordu; madde düzeltildi (kırmızı olan yalnız geliştiricinin yerel Go'su).
> **G2** B1'in *"bu sınıf bir daha doğamaz"* iddiası yanlıştı: numaralı bir atıf
> (*"Sınır 12"*) hayatta kalmıştı, 13 satır altında doğru biçim kullanılırken —
> düzeltildi. **G3** sınır **[`postgres:17-alpine` hareketli etiket]**'in (c) gerekçesi yanlış soruyu cevaplıyordu (digest
> pinleme oran limiti için değil **içerik kimliği** için yapılır) **ve** (a)'nın
> *"iki yazım doğar"* kısmı yalnız benim değerlendirdiğim varyant için doğruydu:
> ölçüldü, `grep 'image:'` **üçünü de** `postgres:17-alpine`'da buluyor, yani üçünü
> birden pinlemek **tek yazım** bırakırdı. Taşıyıcı gerekçe (blast radius bir sağlık
> sondası) öne alındı, digest **açık sertleştirme** olarak duruyor. **G4** ayırt
> edici adresi basıyordu ama *"pod'un kendi adresiyle karşılaştır"* demiyordu (bayat
> EndpointSlice / CoreDNS 30 sn önbelleği bir DNS arızasını *"soket"* diye
> etiketletirdi) — her iki manifeste ve README'ye eklendi. **G5** *"Migration Job'ı ve
> sunucu bugün taşıyor"* **kümede yanlıştı** (canlı şablonlarda `initContainers` yok);
> cümle bir **ölçüm komutuyla** değiştirildi. **G7** dosyanın *"kümedeki tek iki
> NetworkPolicy"* cümlesi kendi kuralı listeye girince yanlışlandı (bugün **üç**).
> **G8** kartın *"toplam 31→30"* özeti ağaçtaki hiçbir değişikliğe karşılık gelmiyordu.
> **G6/G9** kozmetik ve *"tek bilinmeyen"* aşırı iddiası.
> ✅ Genel gözün **zamanlama saldırısı tutmadı** ve denetçi bunu kendisi söyledi:
> hata dalı 120 sn'de ateşliyor, T42'nin penceresi ~3 sn, o senaryoda dal hiç çalışmaz.
>
> **YENİDEN ÖLÇÜM.** Kum havuzu yeniden kuruldu ve yine silindi. Taze kurulum
> **üçüncü kez**: 17 → 26 → **33 sn, `RESTARTS=0`**. Her iki initContainer script'i
> `sh -n` temiz ve **hâlâ birebir aynı** (yalnız son kelime farklı). Her iki hata dalı
> düzenlemeden **sonra** yeniden koşuldu ve doğru dalı bastı.
>
> **🔴 [FAZ C · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED. AYNI KUSUR SINIFININ ÜÇÜNCÜ TEKRARI, VE BU KEZ
> O SINIFI KAPATMAK İÇİN YAZDIĞIM KONTROLÜN İÇİNDE.**
> Sıra şu: (1) kurtarma adımı yalnız **dolu** yolda test edildi, **boş** yol normal
> olandı → KRİTİK. (2) S3'ün v1'i at-risk kümede `ok` diyordu (`zsh` karşılaştırması).
> (3) S3'ün **v2'si dejenere girdide** `ok` diyor. **Üçünün şekli aynı: ölçülmeyen
> yol, gerçekte en olası olan yol.**
>
> **B1 — S3 v2, `secret/ghcr` HİÇ YOKKEN `ok` basıyordu.** Denetçi birebir koşturdu:
> `sec_t=""` → `newest`=`pod_t` → eşitlik testi false → `|| echo 'ok'`. Üç kabukta da
> aynı. **Ve bu, `AT RISK`'ten daha kötü bir durumu yeşil gösteriyordu:** Secret hiç
> yoksa pod yeniden çekemez, ezilmiş bir token bile yoktur. İki ek zayıflık da
> doğrulandı: **hassasiyet farkı cevabı ters çeviriyordu** (`…44Z` vs `…44.500Z` →
> `.` < `Z` → `ok`, yanlış) ve **`managedFields[-1:]` "en yeni" değil**, apiserver o
> diziyi önce Operation'a göre sıralıyor (bugün iki giriş de `Update` olduğu için
> doğru satırı okuyordu — *doğru çıkmak* ile *doğru olmak* aynı şey değil).
> **Çare README parçacığı değil, bir kapı:** `scripts/verify-deployment.sh
> pull-credential`, **tasarım gereği fail-closed** — okunamayan her girdi `UNKNOWN`
> (exit 2), ve `ok` yalnız iki damga da sabit biçime ayrıştırıldıktan sonra basılıyor.
> `managedFields`'ın **tümü** taranıp en yenisi seçiliyor; karşılaştırma
> `YYYYMMDDHHMMSS`'e indirgeniyor, yani hassasiyet farkı cevabı çeviremiyor.
> **On beş vaka gerçek script'e karşı koşuldu** (kubectl stub'ı ile): Secret yok ·
> `managedFields` yok · yalnız boş satır · pod yok · pod listesi boşluk · kubectl
> düşüyor · `startTime` boş · **her iki taraf da okunamıyor** → **sekizi de exit 2**,
> hiçbiri `ok` değil; Secret yeni/eski/eşit · karışık hassasiyet **iki yönde** ·
> `managedFields` ters sırada · iki pod'dan biri riskli → yedisi de doğru.
> Canlı: **`AT RISK`, exit 1**. Sayılmış kalıntı: **aynı saniye** içindeki fark `ok`
> sayılıyor (k8s damgaları zaten saniye hassasiyetinde, ölçüldü).
>
> **B2 — canlı bir deploy kapısı, sorgusu patladığında YEŞİL geçiyordu.**
> `psql`, `-v ON_ERROR_STOP=1` olmadan stdin'den okurken SQL hatasında **exit 0**
> veriyor. Gerçek PostgreSQL **17.10**'a karşı ölçüldü — bozuk sorgu: bayrak yokken
> **exit 0**, bayrakla **exit 3**; sağlıklı sorgu ikisinde de **exit 0**. Yani
> `pg_stat_activity`'nin şeması ya da izni değişseydi kapı **hiçbir şey
> karşılaştırmadan** *"her ağ bağlantısı tappa_app"* diyecekti. Bayrak eklendi **ve
> başarısızlık açıkça yakalanıyor** (`set -e`'ye bırakılmadı): uçtan uca ölçüldü —
> sağlıklı şema → all-clear + exit 0 · sorguyu kasten bozunca → *"NOTHING was
> checked. This is not a pass."* + **exit 2**, ve all-clear satırı **basılmıyor**.
> Bu, dosyanın **kendi başlığının** tarif ettiği kusurun üçüncü örneğiydi.
>
> **D1** kurtarma adımının `kubectl` düşünce deploy'u durdurması **doğru varsayılan**
> ama sayılmamıştı → sınır **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]**. **D2** kartın *"14408 / 20930 bayt"* ölçümü
> **yeniden üretilemiyor** (bugün 14818 / 21340; dosyalar tur boyunca büyüdü) — bayat
> mutlak sayı kaldırıldı, taşıyıcı ölçüt bırakıldı. **D3** *"`tappa.everva.com.tr`
> bugün zaten Cloudflare üzerinden cevap veriyor"* artık **yanlış**: `dig` →
> `144.76.158.60`, kapı → **exit 0 "not proxied"**; joker **hâlâ** proxy'li, yani
> uyarının özü ayakta, düzeltilen yalnız bu host hakkındaki şimdiki zaman.
> **D4** Cloudflare kapısının başarı özeti `^(HTTP|…):` arıyordu ama durum satırı
> `HTTP/2 200` — iki nokta yok, yani **hiçbir zaman basılmıyordu**; `^HTTP/` oldu ve
> canlıda artık görünüyor. **D5** `dnscheck` için temizleme komutu eklendi (`--rm`
> yalnız kubectl bağlıyken siler, komut ise Ctrl-C ihtimali yüksek bir olayda
> koşulur). **D6** README tablosu elimdeki **üç** koşunun **ikisini** gösteriyordu;
> üçü de yazıldı.
>
> **🔴 [FAZ C · 2026-08-16] 6. TUR — YENİ ÜÇÜNCÜ GÖZ RED. AYNI SINIFIN DÖRDÜNCÜ ÖRNEĞİ, AYNI KONTROLÜN
> ÜÇÜNCÜ SÜRÜMÜNDE — VE KAPATMAYI BIRAKIP SAYMAYA GEÇTİM.**
>
> **BULGU.** `check_pull_credential`'ın Secret tarafı, ayrıştıramadığı bir
> `managedFields` girdisini **sessizce atlıyordu** (`continue`), oysa pod tarafı aynı
> girdi sınıfında `return 2` veriyordu. En yeni girdi okunamazsa **eski bir damga
> kazanıyor** ve at-risk küme `ok` çıkıyordu. Yeniden ürettim:
> `SEC_OUT=$'2026-08-14T00:00:00Z\\n\\n'` → **`ok … EXIT=0`**. İkinci kanal: `_norm`
> ilk 14 rakama kestiği için **saat dilimi ofsetini UTC sanıyordu** — pod
> `2026-08-16T09:00:00+02:00` (= 07:00Z) vs secret `08:38:41Z` → gerçek cevap
> **AT RISK**, script **`ok`**. 🔴 **Ve bunların ikisi de, bir önceki turda kendi
> yazdığım *"fail-closed by construction — okuyamadığı HER girdi için UNKNOWN"*
> cümlesini, o cümlenin KIRK SATIR AŞAĞISINDA çürütüyordu.**
>
> **KARAR: 3. YOL — HÜKÜM KALDIRILDI, KANIT KALDI.** Gerekçe brief'in ikinci durma
> kuralı: *"Bir noktadan sonra her koruma bir sonraki turda yeniliyorsa: kapatmayı
> bırak, dürüstçe LİMİT olarak yaz. Sayılmış bir açık, kapatıldığı İDDİA EDİLEN bir
> açıktan güvenlidir."* Aynı korumanın **üç kez** yenilenmesi o noktanın ötesidir.
> Elenen iki yol ve nedenleri: **(1) yapısal kapatma** — bunu bir önceki turda
> **yazdım** ve çürüdü; aynı iddiayı dördüncü kez üretmek, kanıtı değil özgüveni
> tekrarlamak olurdu. **(2) daraltma** — iddiayı gerçeğe indirmek doğru olurdu ama
> tehlikeli olan şey damgalar değil **`ok` kelimesi**: yanlış bir hüküm operatörü
> *aramayı bırakmaya* ikna eder, ve daraltılmış bir hüküm de hâlâ bir hükümdür.
> **Seçilen (3):** komut iki damga kümesini tek biçimde basıyor, kuralı yazıyor,
> karşılaştırmayı **operatöre bırakıyor**. Çıkış kodu yalnız *okuyabildim mi* der
> (`0` = kanıt basıldı, **güvendesin demek değil**; `2` = okunamadı, **hiç kanıt
> basılmadı**). Komut zaten `deploy.yml`'e bağlı değil (19 adımın hiçbiri çağırmıyor),
> yani kaybedilen bir kapı yok.
>
> **VE BU SÜRÜMÜN DOĞRULUK İDDİASI TARTIŞMAYLA DEĞİL SAYIMLA VERİLİYOR** — önceki üç
> sürümün her biri iddiasını akıl yürütmeyle kurmuştu ve her biri kaybetti:
> fonksiyonda `ok`/`AT RISK`/`safe` geçişi **0** · dokuz `return`'ün **dokuzu da**
> `return 2` · iki damga arasında karşılaştırma **yok** (tek `-gt`, `managedFields`
> **girdi sayısı** kontrolü). Bunlar `grep -c` ile doğrulanabilir, savunulması
> gereken cümleler değil.
> **On altı vaka gerçek script'e karşı koşuldu:** v3'ü düşüren iki kanal · Secret yok ·
> `managedFields` boş · girdi/satır sayısı uyuşmuyor · pod yok · yalnız boşluk ·
> `kubectl` düşüyor · zaman aşımı `rc=124` · `startTime` boş · pod adı boş · iki
> haneli yıl · `Z` taşımayan damga · üç pod'un ortası okunamaz → **on dördü de
> exit 2 ve hiçbiri kanıt basmadı**; iki okunabilir girdi kanıt bastı. Ayrıştırma
> artık normalleştirme değil **kabul/ret**: `Z` ile bitmeyen hiçbir damga geçmiyor,
> yani ofset kanalı bir daha *sessizce yanlış* olamaz — reddediliyor.
>
> **U1** README *"İki ayrı taze koşu"* diyordu, tablo **üç** sütun taşıyordu (5. turun
> D6'sı sütunu ekleyip cümleyi bırakmıştı) — 4. turun U5'inin **aynı bölümde** yeniden
> doğuşu; düzeltildi. **U2** kartın kendi içinde numaralı bir sınır atıfı kalmıştı
> (*"sınır 15"*, bugün 16) ve dördü daha bulundu; hepsi **anahtar adla** verildi.
> 🔴 **Ve *"yani bu sınıf bir daha doğamaz"* cümlesi geri çekildi** — iki kez
> yanlışlandı (4. turda G2, 6. turda U2), yani bir ölçüm değil bir tahmindi. Yerine
> dar ve doğru olan yazıldı: adla verilen atıflar numara kaymasından etkilenmez.
> Mekanik kontrol eklendi: dokuz anahtarın tamamı var olan maddeye düşüyor, **sıfır
> sarkan atıf** — bu tarama iki gerçek kusur buldu (`deploy.yml`'de satır sarması
> yüzünden **ikiye bölünmüş** bir anahtar, ve script'te **harf çevrilmiş** bir
> anahtar), ikisi de düzeltildi. **U3** `all` modu `pull-credential` koşmuyordu ve
> hata mesajı onu listelemiyordu: mesaj düzeltildi, `all`'a **eklenmedi** — `all`
> kapıları koşar, bu bir kapı değil, ve *"hepsi koştu"* anlamına gelen bir ada
> vermediği güvenceyi vaat ettirmek bu kartın imza kusurudur.
>
> **🔴 [FAZ C · 2026-08-16] 7. TUR — SINIFIN BEŞİNCİ ÖRNEĞİ: HÜKÜM KODDAN KALKTI,
> RUNBOOK'TA YAŞIYORDU.** Denetçi kod tarafını bağımsız olarak yeniden ölçüp
> kapandığını doğruladı (kendi `grep`'i, kendi fonksiyon sınırı, kendi 10 vakası:
> 8'i exit 2 ve hiç kanıt basmadan). **Tek bloklayan metindi ve ağırdı.**
>
> `README`'nin sınır **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** maddesi
> operatöre *"`pull-credential` koş; **`AT RISK` diyorsa** çare yeni bir deploy"*
> diyordu. O çıktı bu turda **kaldırıldı** — ve **aynı dosya** kırk satır yukarıda
> *"`ok`/`AT RISK`/`safe` geçişi: 0"* diye kendi ölçümünü yazıyordu. Yani README
> kendi içinde çelişiyordu, ve **bekleme kelimesi hiç gelmeyeceği için** operatör
> *"demek ki sorun yok"* deyip kümeyi at-risk hâlde bırakabilirdi — üstelik sınır 
> **[kurtarma adımı …]**'nın tarif ettiği *"yeni token, eski iş yükü"* anında, yani
> komutun **en çok işe yaradığı** anda.
>
> 🔴 **VE ÖNEMLİ OLAN ŞU: bu turda yazdığım mekanik tarama bunu YAKALAYAMAZDI.**
> Tarayıcı *köşeli parantezli anahtar atıflarını* sayıyordu, **hüküm sözcüklerini**
> değil — yani tarayıcıyı **bir önceki kusurun şekline** göre yazmıştım, o turun
> **ürettiği** şekle göre değil. Sınıfın beş tur boyunca hayatta kalma mekanizması
> tam olarak bu. Doğru tarama (`grep -rn 'AT RISK\|AT-RISK'`, operatörün okuduğu
> **her** dosyada) koşuldu: `README:298` ölçümün kendisi ✔ · `verify-deployment.sh`
> ve kartta tarih anlatımı ✔ · `deploy.yml` ve manifestlerde **0** ✔ · tek talimat
> `README:639`'daydı ve **kaldırıldı**. Sayım da README'ye taşındı — sayılan bir
> açığın güvenli olması, sayımın **operatörün okuduğu yerde** durmasına bağlı.
>
> **C1 — `all` modu ilk kapı düşünce ikinciyi hiç koşmuyordu.** `set -e` altında
> `check_cloudflare …; check_db_role …`. Ölçüldü: proxy'li host sahtelenince
> `db-role` **hiç çalışmadı**. Fail-open değil (koşu kırmızı) ama *"hepsi"* adı
> sağlamadığı bir bütünlük vaat ediyordu. İkisi de koşuyor, sonuçlar toplanıyor,
> özet satırı basılıyor; çıkış kodu **ihlal (1) > okunamadı (2) > temiz (0)**.
> Dördü de ölçüldü: `VIOLATION+pass→1` · `pass+pass→0` · `pass+UNCHECKED→2` ·
> `VIOLATION+UNCHECKED→1`. ⚠️ İlk yazımım bozuktu — `"$( )"` içine gömülü `case`
> çevreleyen `case`'i bozdu ve özet satırı **kendi kaynak metnini** bastı; ölçüm
> yakaladı, adlandırılmış bir yardımcıya çevrildi.
> **C5** `db-role`'ün özet sorgusu düşerse mesajsız `exit 3` ile ölüyordu (fail-closed
> ama açıklamasız); artık *"the offender query passed but the summary query did not
> run, so this result is NOT trustworthy"* + **exit 2**. İki yol da ölçüldü.
> **C2** *"kartın içindeki numaralı atıfların hepsi çevrildi"* iddiam eksikti — iki
> tane daha vardı (`:509`, `:569`), ikisi de çevrildi; kalan dört geçiş **geri çekme
> metninin kendisi**. **C3** emsal 1 sn kayıktı: canlı Job'dan okundu
> (`startTime 08:38:54Z` → `Failed 08:39:32Z`) = **38 sn**, 37 değil.
> **C4** defterde **iki blok aynı numarayı** taşıyordu (*"4. TUR"*) ve 3/4 sırasız
> görünüyordu; her başlık artık **hangi yarıya ait olduğunu ve tarihini** taşıyor
> (`[PAKETLEME yarısı · 2026-08-15]` / `[FAZ C · 2026-08-16]`), ve sıranın neden
> böyle olduğu yazıldı — bu defteri sıfır hafızalı bir ajan okuyacak.
>
> **✅ [FAZ C · 2026-08-16] 8. TUR — `tappa-security-auditor` ONAY.** (2. turun
> kritiğini bulan mercek.) Üç bulgu, üçü de tek satırlık, üçü de kapatıldı.
>
> **§4.7 (ORTA) — yakalanan stderr basılıyordu.** `kubectl`'in jsonpath yazıcısı bir
> **şablon çalıştırma hatasında** şablonu değil **nesnenin tamamını** stderr'e döküyor.
> Mekanizma zararsız bir ConfigMap üzerinde ölçüldü (Secret'a **dokunulmadan**):
> `-o jsonpath='{.data[0]}'` → *"object given to jsonpath engine was: …
> "ca.crt":"-----BEGIN CERT…"*. `secret/ghcr` için o içerik `.dockerconfigjson`, yani
> **canlı çekme token'ı**, ve basıldığı an `deploy.yml`'in kendi koyduğu *"Report the
> SHAPE and never the value"* kuralı delinirdi. **Ulaşılabilirlik kanıtlanamadı**
> (gerçek bir Secret'ın `managedFields`'ı daima dizidir, yani dökümü tetikleyen tip
> uyuşmazlığı bariz biçimde erişilebilir değil) — ama `README`'nin kalıcı çaresi
> *"uzun ömürlü `read:packages` PAT"*, ve **o gün geldiğinde bu kanal iptal edilmiş
> bir iş token'ı değil YAŞAYAN bir kimlik taşır**. Üç `kubectl` çağrısının **üçü de**
> (denetçi ikisini işaretlemişti; üçüncüsü pod listesi, aynı kalıp, aynı muamele —
> dosyada tek kural olsun diye) `2>/dev/null` + sabit metne çevrildi. Ölçüldü, sahte
> bir token döken stub ile: **eski yazım 1 kez sızdırıyor, yeni yazım 0**; canlı yol
> hâlâ çalışıyor ve çıktısında `dockerconfigjson` **0**. Taranan dördüncü yakalama
> (`curl`) **değiştirilmedi ve gerekçesi yazıldı**: farklı araç, nesne dökümü değil
> mesaj basıyor, ve bu turun eklediği bir satır değil.
>
> **§4.6 (DÜŞÜK) — `delete pod` bayrağı.** `get` ile `delete` arasında pod kaybolursa
> NotFound → exit 1 → `set -e` → adım kırmızı; ve adım tam olarak 2. turun kritiğinin
> durduğu yerde, `secret/ghcr` ezildikten **sonra**, hiçbir `apply`'dan **önce**. Yani
> deploy **iyi huylu** bir sebeple kümeyi *"yeni token, eski iş yükü"* durumunda
> bırakıyordu. Ölçüldü: bayraksız `Error from server (NotFound)` **exit 1**,
> `--ignore-not-found` ile **exit 0**. Aynı dosya bu sınıfı 68 satır aşağıda zaten
> tanıyordu. `README` sınırı da güncellendi: **`delete` yarısı kapatıldı**, `get`
> yarısı sayılmış kalmaya devam ediyor (orada kırmızı olmak doğru).
>
> **§4.5 (DÜŞÜK) — uygulama pod'u artık `tappa_owner` adını ağa yazmıyor.** Kriter
> ihlal edilmiyordu (kimlik doğrulaması yok, DSN yok, parola yok) ama iki bedeli
> vardı ve ikincisi ağırdı: `verify-deployment.sh`'in `db-role` kapısı
> `usename='tappa_owner' AND client_addr NOT NULL` üzerine kurulu ve mesajı *"tenant
> isolation is void"* diyor — yani **ürünün kendi sağlık sondası**, §4.5'i bekleyen
> tek kapıya bir yanlış alarm kanalı açıyordu. Rol adı yalnız bir dize olduğu için
> `-U tappa_app` bedava aynı işi görüyor. **Ölçüldü** (docker, `tappa_app` rolü o
> veritabanında **hiç yokken**): `-U tappa_app` → **`accepting connections`, exit 0** ·
> `-U tappa_owner` → aynı · **`-U` hiç yokken, uid 65532** → `no attempt`, **exit 3**
> (yani bayrağın kendisi hâlâ zorunlu) · ve hiçbir sonda arkasında **backend
> bırakmıyor**. `30-migrate-job.yaml` **`tappa_owner` ile kaldı** — o pod gerçekten o
> rol. İki script bunun dışında hâlâ birebir aynı.
>
> **VE DENETÇİ KARTIN AÇIK BİR BULGUSUNU DARALTTI — buraya yazılıyor.** Kartın
> *"`redline-check.sh`'in `SRC` listesi `deploy/`'u görmüyor"* maddesi bu turda
> **yanlış yeri** işaret ediyor: bu turun tek gerçek §4.7 kanalı **zaten taranan**
> bir yolda (`scripts/`) duruyordu ve tarayıcı **yine de göremezdi**, çünkü R7 deseni
> Go'ya özgü (`(slog|log|fmt)\\.[A-Za-z]+\\(`) ve bir kabuğun
> `echo "$captured_stderr"` şeklini **hiç tanımıyor**. **Eksik olan dizin değil,
> desen şekli** — **ve bu cümle YALNIZ R7 İÇİN geçerlidir, kapsamı burada yazılıyor.**
> Ölçüldü: `deploy/`'a R7 deseninin eşleşmesi **0**, yani bu turun kanalı `SRC`
> genişletilse de görünmezdi. **Ama `SRC`'ye `deploy/` eklemek "hiçbir şey bulmaz"
> DEĞİLDİR:** R5'in `DATABASE_MIGRATE_URL` deseni `deploy/` + `Dockerfile`'da **11**,
> N1'in Node deseni **1** eşleşme veriyor — yani bu kartın **700 satır yukarıdaki**
> *"naif bir genişletme 12 yanlış pozitif üretiyor"* ölçümünün **aynısı**, ve o
> ölçüm hâlâ geçerli. İki cümle çelişmesin diye: R7 için genişletme yetmez (desen
> Go'ya özgü ve bir kabuğun `echo "$captured_stderr"` şeklini tanımıyor); R5/N1 için
> genişletme **erken**, çünkü önce o 12 yanlış pozitifin ne anlama geldiğine karar
> verilmeli. Orkestratöre devredilen madde bu iki cümleyle birlikte okunmalı.
>
> **🔴 8. TURUN İKİ KENDİ HATASI — DEFTERE, SOHBETE DEĞİL.** Kartın kendi emsali
> (*"SÜREÇ İHLALİ, KÖK NEDENİYLE"* ve 4. turun `B4`'ü) bunları kaydediyor; bu tur da
> kaydetmeli, yoksa defter yalnız başarıyı taşır.
> **(a) SALT-OKUMA SINIRI AŞILDI.** `--ignore-not-found`'un davranışını ölçmek için
> `ns/tappa`'ya **iki kez `kubectl delete pod`** koşturdum. Ad kasten var olmayan bir
> addı (`definitely-not-a-real-pod-9x7q`), yani **hiçbir şey mutasyona uğramadı** —
> orkestratör kümeyi doğruladı (5 pod, 1 netpol, canlı 200/200, zarar yok) — ama
> `delete` fiili o namespace için **yasaktı** ve ölçüm bir kum havuzunda yapılmalıydı.
> Kural, sonucun iyi çıkmasıyla değil, fiilin kendisiyle ilgilidir.
> **(b) BİR DOĞRULAMA GEÇERSİZDİ VE ÖNCE FARK EDİLMEDİ.** İlk `-U tappa_app` ölçümümde
> konteyner IP'sini `{{.NetworkSettings.IPAddress}}` ile aradım, boş döndü, ve **iki
> sonda da boş bir host'a** gitti: ikisi de `no response`/exit 2 verdi, yani *"her iki
> rol de çalışmıyor"* gibi görünen simetrik ve **anlamsız** bir sonuç. Simetri beni
> uyardı; olmasaydı bu, ölçmediğim bir şeyi ölçtüm sanacağım bir sonuçtu. Ayrı bir
> docker ağıyla yeniden koşuldu. **Ders 4. turun B4'ünün aynısı: bir ölçümün ÇIKTI
> ÜRETMESİ, doğru şeyi ölçtüğü anlamına gelmez.**
>
> **✅ [FAZ C · 2026-08-16] 9. TUR — SON. Üçüncü göz ONAY, güvenlik denetçisi ONAY,
> birinci durma kuralı karşılandı** (art arda iki tur davranış kusuru yok; denetçi
> §4.7 sınıfının **altıncı örneğini kodda aradı ve bulamadı**, ve en olası adayı —
> `deploy.yml`'in `secret/ghcr` üzerindeki yönlendirilmemiş `go-template` çağrıları —
> **ölçümle eledi**: go-template yazıcısı nesne dökmüyor). Altı metin/kabuk maddesi:
> **B1** bu turda eklediğim teşhis komutu **yazıldığı gibi çalışmıyordu** —
> `get svc tappa-postgres endpointslice` her iki adı da `svc` kaynağında arıyor →
> `services "endpointslice" not found`, **exit 1**, yani bölüm 5'in *"EndpointSlice
> boş"* teşhisi hiç yapılamıyordu **ve hata aranan arızanın kendisi gibi
> okunabiliyordu** — üstelik bölümün başında *"her satır fiilen ölçüldü"* yazıyor.
> İki ayrı çağrıya bölündü (ölçüldü: ikisi de exit 0).
> **B2** `all` modu **ölçemediği kapıyı `pass` diye etiketliyordu**: ad çözülmediğinde
> cloudflare *"Nothing about Cloudflare could be checked"* deyip 0 dönüyor, `all` de
> `cloudflare=pass`, **exit 0** basıyordu — kendi yorumu *"2 if any gate could not
> CHECK"* derken. Aynanın öteki yüzü: curl zaman aşımı (rc=28) → `VIOLATION`, oysa o
> da ölçememe. Artık kelime **`CF_OUTCOME`**'dan geliyor, çıkış kodundan değil; altı
> kombinasyon ölçüldü (`pass/pass→0` · `VIOLATION/pass→1` · `UNCHECKED/pass→2` ·
> `timeout→UNCHECKED/2` · `pass/UNCHECKED→2` · `UNCHECKED/UNCHECKED→2`) ve
> **`cloudflare` modunun kendi sözleşmesi değişmedi** (0/1/0/1 — `deploy.yml` ona
> bağlı, özellikle *"ad çözülmüyor → 0"* ilk deploy için).
> **U3** `-c wait-for-postgres` komutları bugünkü kümede `container … is not valid
> for pod … out of: goose` veriyor (FAZ C henüz inmedi); uyarı 50 satır ötedeydi ve
> bölüm 4'te hiç yoktu — **her iki komut bloğunun yanına** taşındı.
> **U4** `README:95`'in doğrulama komutu sağlıklı durumda **sıfır çıktı + exit 1**
> veriyordu (origin `server` başlığı göndermiyor, durum satırında `HTTP` sonrası iki
> nokta yok) — `^HTTP/` biçimine hizalandı, ölçüldü: `HTTP/2 200`, exit 0.
> **U1** kart kendi ölçümüyle çelişiyordu ve **cümle R7'ye daraltıldı** (yukarıda).
> **U2** bu iki kendi hatam yukarıya yazıldı.
> ⚠️ **Denetçinin doğrulayamadığı tek şey kayda geçsin:** `--ignore-not-found`'un
> bayrak semantiği salt-okuma sınırı yüzünden **denetçi tarafından koşturulmadı**;
> akış stub matrisiyle doğrulandı, bayrağın kendisi yalnız benim ölçümümle.
>
> **KAPSAM DIŞI BIRAKILAN, BİLİNÇLİ:** kartın *"Yedek: otomatik, geri yükleme
> denenmiş"* ve *"KEK dönme aracı"* kriterleri **hâlâ açık** (FAZ D); `README`'nin
> sınır listesinde **[yedek YOK]** ve *"KEK döndürme aracı YOK"* olarak duruyorlar
> ve **kaldırılmadılar**.
>
> **KUM HAVUZU SAPMALARI (dördü de yazılı):** namespace `tappa-verify` · PVC
> 20Gi→2Gi (node diski; ölçülen şey ağ ve sıra, depolama değil) · atılabilir sırlar
> (`openssl rand`, değerler hiç basılmadı) · 🔴 **iki ürün imajı `postgres:17-alpine`
> stand-in'leriyle değiştirildi**, çünkü yukarıdaki KEP-2535 ölçümü gereği kum havuzu
> pod'ları düğümdeki private imajları **kullanamıyor** ve `read:packages` yetkili bir
> token yok (yerel `gh` token'ının kapsamları: `gist, read:org, repo, workflow`;
> GHCR manifest isteği → **401**). Stand-in'ler test edilen özelliği koruyor
> (açılışta hemen DB'ye bağlan, başarısızsa çık) ama **goose ikilisinin kendisini
> kanıtlamıyor** — o zaten 2026-08-15'te kural silindiğinde 20 migration'ı geçmişti.
> `40-ingress.yaml` kum havuzunda **uygulanmadı** (canlı host ile çakışırdı).
>
> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ D — "deploy YOLU doğru olsun"): T44 KAPANDI,
> T43 KÖKÜNDEN KALDIRILDI, VE BU BLOK YUKARIDAKİ ÜÇ CÜMLEYİ ADIYLA İPTAL EDİYOR.**
> Değişen dosyalar: **yeni** `deploy/k8s/01-rbac.yaml` · `.github/workflows/deploy.yml` ·
> `deploy/k8s/{20-app,30-migrate-job}.yaml` · `deploy/README.md` ·
> `scripts/verify-deployment.sh` · `Dockerfile` (yalnız başlık yorumu). **Go kodu
> değişmedi** (`git status --short` → 0 adet `.go`), `ci.yml` **değişmedi**. Canlı ürün
> üç ayrı anda ölçüldü: `/healthz` **200**, `/readyz` **200**. Küme mutasyonu yalnız
> kum havuzu `tappa-verify`'de yapıldı ve namespace iş sonunda **silindi** (artık
> kalıntı: `ns` → NotFound, `clusterrole|clusterrolebinding` eşleşmesi **0**);
> `ns/tappa`'ya hiçbir yazma yapılmadı.
>
> **İŞ 1 — T44: RBAC ARTIK BİR DOSYA, VE ÖLÇEREK DARALTILDI.**
> Elle yaratılmış (yalnız kümede yaşayan) beş nesne repoya alındı. ⚠️ **Orkestratörün
> ölçümü eksikti ve düzeltiliyor: üç değil BEŞ nesne var** — brief `Role`, `RoleBinding`
> ve `ServiceAccount`'u sayıyordu; kümede ayrıca `ClusterRole/tappa-namespace-only` ve
> onun `ClusterRoleBinding`'i de vardı (`namespaces`, `resourceNames: [tappa]`,
> `get,patch,update`). Beşi de `01-rbac.yaml`'a girdi.
>
> **Daraltma, satır satır ve ölçümle.** Yöntem: kum havuzunda **yalnız önerilen
> kurallara** bağlı bir ServiceAccount yaratıldı ve `deploy.yml`'in küme çağrıları
> o kimlikle (`--as=...`) baştan sona koşuldu.
> ⚠️ **Burada bir tur boyunca *"deploy.yml'in on beş küme çağrısı"* yazdı ve bu iki
> şeyi karıştırıyordu.** **15**, benim prova betiğimde numaralandırdığım **adım
> sayısıydı** (`01`…`15`), `deploy.yml`'in çağrı sayısı değil. Doğru sayı, yöntemiyle
> birlikte: her `run:` gövdesi ayrıştırılıp yorumlar atılarak, **API sunucusuna giden**
> `kubectl` çağrıları sayıldı — `kubectl version --client`, `kubectl config
> current-context` ve `--dry-run=client` ile yerelde render edilenler **hariç**;
> `verify-deployment.sh db-role`'un `exec`'i **dahil**. Sonuç **19**: adım 14→2 ·
> 15→5 · 16→5 · 17→3 · 18→1 · 19→3. (Bağımsız denetçi de aynı 19'u saydı.) Prova
> betiğinin 15 adımı bu 19 çağrının **hepsinin şeklini** kapsıyordu — bir `for`
> döngüsündeki iki `logs` çağrısı ve iki `get job` yoklaması provada birer kez
> koşuldu — ama *"on beş çağrı"* demek yanlıştı. Kaldırılanlar ve *neden gerekmediği*:
>
> | Çıkarılan | Neden — hangi adım istemiyor |
> |---|---|
> | `secrets` create/update/patch/delete/**list**/watch | Secret yazan adım (**"Write the GHCR pull credential"**) kalktı; geriye tek varlık kontrolü kaldı → `get` + `resourceNames: [tappa-secrets]`. Ölçüm: `get tappa-secrets` → NotFound (yetkili) · `get some-other-secret` → **Forbidden** · `get secrets` (list) → **Forbidden** |
> | `pods` create/update/patch/watch/**get** | Hiçbir adım Pod yaratmıyor (Pod'ları denetleyiciler yaratır, kendi kimlikleriyle). `get` de gereksiz çıktı: `logs job/…`, `exec statefulset/…` ve `get pods` **üçü de LIST üzerinden** çözüyor — yalnız `list` bırakılıp üçü de koşturuldu, üçü de exit 0 |
> | `pods` **delete** | Onu kullanan adım (takılmış-pod kurtarma) kalktı. `delete job --wait=true` yine de çalışıyor: Job'ın pod'larını **çöp toplayıcı** siliyor (ölçüldü, kimlikte pod-delete yokken exit 0) |
> | `persistentvolumeclaims` (yedi fiil) | PVC `volumeClaimTemplates`'ten doğar, statefulset denetleyicisi yaratır. Kum havuzunun StatefulSet'i volumeClaimTemplate taşıyordu ve **hiç PVC yetkisi olmadan** uygulandı |
> | `serviceaccounts` (yedi fiil) | `deploy.yml` hiçbir ServiceAccount'a dokunmuyor |
> | `events` (yedi fiil) | Hiçbir adım event okumuyor; `kubectl describe` dosyada **geçmiyor** |
> | `replicasets` (yedi fiil) | `rollout history`/`undo` içindir, ikisi de iş akışında yok. `rollout status deployment` ve `logs deployment/…` **replicaset yetkisi olmadan** koştu |
> | `deployments/status`, `statefulsets/status` | Status alt kaynağını denetleyiciler yazar; `rollout status` `.status`'ü **ana kaynaktan** okur |
> | `external-secrets.io/externalsecrets` | `deploy/examples/externalsecret.example.yaml` **örnektir**; `deploy.yml` yalnız `deploy/k8s/` altındaki adlı dosyaları uyguluyor ve hiçbiri ExternalSecret değil |
> | `jobs` **patch** | Migration adımı Job'ı **önce siliyor** (`--wait=true`), yani apply daima `create`. Kaldırmanın bedeli ölçüldü ve **sonucu değiştirmiyor**: Job silinemeden kalmışsa `patch`'siz *"cannot patch resource jobs"* (exit 1), `patch`'liyse API'nin *"spec.template … is immutable"*'ı (exit 1) |
> | `namespaces` **create** | Namespace operatörün (RBAC onun içine giriyor). `deploy.yml` 00-namespace.yaml'ı **var olan** nesneye yamalıyor |
>
> **Bırakılan her yetkinin sahibi bir adım var** ve dosyada her kuralın üstünde adıyla
> yazılı. İkisi kolay kaçırılıyor, o yüzden burada da:
> **(1) `apps` üzerinde `watch` ZORUNLU ve yokluğu FAIL-OPEN.** Üç ölçüm, sağlıklı ve
> bitmiş bir rollout üstünde: `get,create,patch` → `rollout status` **exit 1, "timed out
> waiting for the condition"** (sağlıklı deployment'ta!) · `get,list,create,patch` →
> **exit 0 ama** stderr'de `Failed to watch … is forbidden` — yani **hiçbir şey
> izlemeden** ilk LIST ile "başarılı" dedi · `get,list,watch,create,patch` → exit 0,
> gerçek. Ortadaki satır tehlikeli olan.
> **(2) `namespaces` üzerinde `patch` ZORUNLU, ve saf test bunu GİZLİYOR.** Değişmemiş
> bir `00-namespace.yaml`'ı yalnız `get` ile apply etmek **"unchanged", exit 0** verir
> (kubectl boş yama hesaplayıp hiç göndermez). Gerçekten değişen bir etiketle: `get`
> tek başına **exit 1**, `get,patch` → **"configured", exit 0**.
>
> **`pods/exec` KARARI — ölçümle verildi, ve verilen karar (i)'dir.**
> Kapı doğduğu günden beri **hiç koşmamıştı**: `pods/exec` ayrı bir alt kaynak ve elle
> yazılmış rolde yoktu; önceki her deploy migration'da düştüğü için 19. adıma hiç
> ulaşılmamıştı. **Elenen (ii) — kapıyı exec'siz yeniden yazmak:** hiçbir yetki
> kaldırmıyor (kimlik zaten `jobs: create` taşıyor, migration'ın kendisi budur) ve
> bedeli somut: iş akışının içinde yazılmış yeni bir pod spec'i, veritabanı parolasının
> **CI'ın yazdığı bir manifestten** geçmesi (bugün olmayan **yeni** bir §4.7 kanalı —
> exec biçimi `PGPASSWORD`'ü Postgres konteynerinin **kendi ortamından** okuyor ve ona
> hiç dokunmuyor), bir bekleme döngüsü, log okuma ve temizlik: **beş RED'i kabuk
> dalları yüzünden almış** bir dosyada ~40 satır yeni kabuk. **Seçilen (i)** ama
> `resourceNames: ["tappa-postgres-0"]` ile: exec **tek pod adına** kilitli. Ölçüldü:
> `exec statefulset/tappa-postgres` → **exit 0**; `exec deployment/tappa` →
> **Forbidden** — yani `TAPPA_TAG_KEK`'i ortamında taşıyan uygulama pod'una kabuk
> **yok**. Set ölçeklenirse yeni ordinal reddedilir ve kapı **fail-closed** olur.
>
> 🔴 **MARJİNAL-YETKİ ARGÜMANI HİSSİYAT DEĞİL, UÇTAN UCA ÖLÇÜLDÜ — ve sonucu bir
> SINIR olarak yazıldı.** Kum havuzunda, **yalnız** daraltılmış kurallara bağlı
> kimlikle: `secretKeyRef` ile bir Secret'a bakan bir `Job` yaratıldı ve o Secret'ın
> değeri `pods/log` üzerinden **okundu** — üstelik aynı kimlik o Secret'ı
> **listeleyemiyor**. Yani bir Job'ın pod şablonu kendi namespace'indeki her Secret'a
> referans verebiliyor (bu referansın RBAC'i yok, kubelet çözüyor) ve deploy,
> başarısız migration'ı raporlamak için o logu okumak **zorunda**. **Sonuç:
> `secrets: get` (tek isim) ve `pods/exec: create` (tek pod) yeni bir erişim
> AÇMIYOR;** daraltmanın kazandırdığı, yıkıcı yarının kalkmasıdır — kimlik artık
> `tappa-secrets`'ı yaratamaz, ezemez, silemez, yani **yeniden üretilemeyen** tek
> değeri yok edemez. `deploy/README.md` sınır
> **[deploy kimliği namespace'in sırlarını okuyabilir]**.
>
> 🔴 **VE BU TURUN KENDİ ÖLÇÜM TUZAĞI, DEFTERE:** `kubectl auth can-i` **alt
> kaynakları görmüyor** ve "yes" diyor. `can-i create pods/exec` → **yes** ·
> `pods/portforward` → **yes** · **`pods/nonsense` → yes** (kontrol: öyle bir alt
> kaynak yok). Doğrusu `can-i create pods --subresource=exec` → **no**, kontrolü
> `--subresource=nonsense` → **no**. İlk ölçümüm eğik çizgili biçimdeydi ve *"yetki
> zaten var"* diyordu — kontrol sorusu olmasa **yanlış bir sonuca** varacaktım. Bu
> yüzden `deploy.yml`'e `auth can-i` ön kontrolü **eklenmedi** (README sınır
> **[`kubectl auth can-i` alt kaynakları GÖRMÜYOR]**); yokluğun teşhisi bunun yerine
> ilk küme çağrısının **mesajına** kondu (namespace apply'ı artık düşerse
> `01-rbac.yaml`'ı adıyla söylüyor). ⚠️ Kural değiştirmek deploy'un yetkisinde
> **değil** ve öyle kalıyor: bu dosya **operatörün**, `tappa-secrets` gibi.
>
> **İŞ 2 — T43 KÖKÜNDEN KALKTI: DOCKER HUB, PUBLIC.**
> FAZ C'nin **ayırt edici kontrolü** zaten cevaptı: **public** imaj + `imagePullPolicy:
> Never` → **düğüm önbelleğinden açıldı, exit 0**, oysa private imaj kimlik yokken
> **401**. Public depoda **kaydedilecek kimlik yoktur**, yani KEP-2535 kusuru tanım
> gereği oluşamaz. Kazanılanlar tek tek: *"Write the GHCR pull credential"* adımı
> **tamamen kalktı** (bir Secret yaratma yolu daha az, §4.7) · iki pod spec'inden
> `imagePullSecrets` **kalktı** (render edilmiş spec'lerde alan **ABSENT**) ·
> `secret/ghcr` yok → bayat kimlik penceresi yok, `kubectl rollout undo` **401
> almıyor** · `permissions: packages: write` **kalktı** · `:main` etiketi artık
> **build de push da edilmiyor** (hiçbir şey referans etmiyordu; public bir registry'de
> hareketli etiket, reponun kendi adını koyduğu kusur sınıfı).
> **Takılmış-pod kurtarma adımı: ÖLÇÜLDÜ, GEREKMİYOR, KALDIRILDI.** Adımın yazılı
> öncülü *"sır tazelendi ama kubelet'in geri çekilme aralığı büyüdü"*ydü; sır yoksa o
> pencere de yok. Kalan `ImagePullBackOff` sebepleri (depo private kalmış · `429` ·
> etiket yok) bu adımla **düzelmiyor** ve zaten adım `apply`'lardan **önce** koştuğu
> için yalnız **önceki** deploy'un pod'larını görüyordu — yeni sha zaten yeni bir
> ReplicaSet ve yeni bir pod demek. Premisi yanlış olan **yıkıcı** bir adımı tutmak
> kartın imza kusuru olurdu. Bununla birlikte README sınırı
> **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** de kalktı.
> **`scripts/verify-deployment.sh pull-credential` de kaldırıldı** — konusu var olmayan
> bir Secret'tı, yani her sağlıklı kümede *"UNREADABLE"* basacaktı: var olmayan bir
> arıza hakkında alarm. `cloudflare` ve `db-role` modlarının sözleşmeleri
> **değişmedi**.
> ⚠️ **YERİNE GEÇEN SAYILMIŞ SINIR:** Docker Hub anonim çekme bütçesi
> (`x-ratelimit-limit: 100;w=3600`, IP başına) artık **ürün imajlarını da** kapsıyor →
> README sınır **[Docker Hub anonim çekme bütçesi]**.
>
> **🔴 BU BLOK ÜÇ CÜMLEYİ ADIYLA İPTAL EDİYOR** (kartın iki yerinde çelişik metin
> kalmasın):
> 1. FAZ C'nin *"Hafifletme (çare değil): `deploy.yml`'e **Recover pods stuck on a
>    stale pull credential** adımı"* cümlesi — **o adım artık YOK**. FAZ C'de doğruydu
>    ve orada doğru kalıyor; bugünkü dosyayı tarif etmiyor.
> 2. FAZ C'nin *"`verify-deployment.sh pull-credential` iki damgayı kanıt olarak
>    basıyor"* cümlesi — **o mod artık YOK**.
> 3. T43'ün *"kalıcı çare hâlâ kullanıcının: paketleri public yap ya da `read:packages`
>    PAT"* cümlesi — **çare uygulandı, ama GHCR'da değil**: GHCR görünürlüğünün API'si
>    yok (üç kez ölçüldü: `PATCH /user/packages/container/tappa` → **404**;
>    `gh api /user/packages/container/tappa` → **403**) ve kullanıcı arayüzden iki kez
>    denedi. Kullanıcı kararı **Docker Hub public** oldu; AWS ECR elendi (private ECR
>    aynı kimlik sorununu dönen bir IAM token'ıyla geri getirir, ECR Public yalnız
>    `us-east-1`'den push kabul eder ve bir AWS hesabı ister).
> 🔴 **EMEKLİ EDİLEN İKİ SINIR ANAHTARI — grep'in bir yere düşmesi için burada.**
> `deploy/README.md`'nin sınır listesinde artık **YOK**: **[GHCR çekme kimliği
> ömürlü]** (yerine → **[Docker Hub anonim çekme bütçesi]**, aynı 12. sırada) ve
> **[kurtarma adımı kubectl'e bağlı ve FAIL-CLOSED]** (tarif ettiği adım silindi;
> madde kaldırıldı, liste `1..17` ardışık kaldı). Bu defterin **yukarıdaki FAZ C
> blokları** ikisini de hâlâ adıyla anıyor ve **öyle kalıyor** — onlar tarihli
> kayıtlar ve yazıldıkları gün doğruydular; bugünkü listeyi tarif etmiyorlar. Yeni
> eklenenler: **[deploy kimliği namespace'in sırlarını okuyabilir]** (16),
> **[`kubectl auth can-i` alt kaynakları GÖRMÜYOR]** (17); 8. madde
> **[kubeconfig'in kimliği doğrulanmadı]** adını kazandı.
>
> **DEĞİŞMEYENLER, açıkça:** ölçülmüş karar #2 (**migration `Job`, initContainer
> değil**) ayakta — `DATABASE_MIGRATE_URL` uygulama pod'unun spec'inde **yok**
> (render edilmiş spec'te env anahtarları: `DATABASE_URL`, `TAPPA_SESSION_HMAC_KEY`,
> `TAPPA_TAG_KEK`, `TAPPA_INVITE_HMAC_KEY`), sır **anahtar anahtar** veriliyor
> (`envFrom: secretRef` **yok**); değişmez `sha-<12hex>` etiketi **tek** etiket;
> `verify-image.sh`'in **iki kapısı da** push'tan **önce** çağrılıyor (adım 7, 8 →
> adım 9); `wait-for-postgres` initContainer'ları ve `12-networkpolicy.yaml`
> **aynen** duruyor (`git diff --stat deploy/k8s/12-networkpolicy.yaml` → boş).
>
> 🔴 **KULLANICI EYLEMİ GEREKİYOR — üç madde, deploy bunlarsız koşmaz:**
> **(a)** `deploy/k8s/00-namespace.yaml` + `deploy/k8s/01-rbac.yaml`'ı **cluster-admin**
> kubeconfig ile bir kez `apply` et (deploy kendi rolünü değiştiremez — ve bu doğru).
> **(b)** GitHub deposuna **`DOCKERHUB_USERNAME`** ve **`DOCKERHUB_TOKEN`** secret'ları
> (Docker Hub → Account Settings → Personal access tokens, **Read & Write**). Ajan
> bunları **oluşturamaz** ve denemedi; bugün depoda **yalnız `KUBE_CONFIG`** var
> (`gh secret list` ile ölçüldü).
> **(c)** İlk push'tan sonra `atknatk/tappa` ve `atknatk/tappa-migrate` depolarını
> Docker Hub'da **Public** yap — ilk push depoyu **private** yaratır ve private kalırsa
> KEP-2535 kusuru olduğu gibi geri gelir.
> ⚠️ **Secret yokluğu yolu ölçüldü:** tanımsız bir GitHub secret'ı **boş dizeye**
> dönüşür, hata vermez; `docker login` o değerle **rc=1** ve `username is empty` /
> `password is empty` diyor — hangi secret'ın eksik olduğunu da nereden alınacağını da
> **söylemiyor**. Bu yüzden iş akışının **birinci** adımı ikisini de sayıyor ve eksikse
> **hiçbir şey inşa etmeden** duruyor. Sekiz dejenere girdi koşuldu (ikisi de tanımsız ·
> ikisi de boş · yalnız biri · **tek boşluk** · **sekme+satırsonu** · sondaki satırsonu ·
> ikisi de dolu) → **altısı exit 1 ve adıyla mesaj, ikisi exit 0**.
>
> **🔴 [FAZ D · 2026-08-16] 2. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BİR SAYI YANLIŞTI, VE ASIL
> İŞ BİR GARANTİYE MEKANİZMA VERMEKTİ.**
>
> **B1 (bloklayan) — `deploy/README.md`'de ÖLÇÜLMÜŞ BİR SAYI YANLIŞTI, ve aynı
> cümlede ikinci kez.** *"14 nesne … ve **beşi küme kapsamlı RBAC**"* yazmıştım.
> `kubectl api-resources`'ın kendi kapsam alanıyla ölçüldü: **14 nesnenin 3'ü** küme
> kapsamlı (`Namespace/tappa`, `ClusterRole/tappa-namespace-only`,
> `ClusterRoleBinding/tappa-namespace-only`); `01-rbac.yaml`'ın getirdiği **beş**
> nesnenin **yalnız ikisi** küme kapsamlı, **dördü** `rbac.authorization.k8s.io/v1`
> grubunda ve `ServiceAccount` düz `v1`. *"Beş yeni nesne"* ile *"beş küme-kapsamlı
> nesne"* birbirine karışmıştı. ⚠️ **Kusurun ağırlığı sayıda değil yerinde:** o
> paragrafın **var olma sebebi** bir önceki yanlış sayıydı (2026-08-15 denetimi), yani
> aynı hata aynı paragrafta tekrarlandı. Düzeltildi ve gerekçesi paragrafın içine
> ölçümüyle yazıldı.
>
> **B2 — MEKANİZMASI OLMAYAN GARANTİYE MEKANİZMA VERİLDİ: yeni kapı,
> *"Gate — both images must be pullable with NO credential"*.** Denetçi ölçtü:
> `hub.docker.com/v2/repositories/atknatk/tappa/` → **404**, `…/tappa-migrate/` →
> **404** — **iki depo da yok**, yani ilk push ikisini de **private** yaratacak ve
> KEP-2535 kusuru aynen geri gelecekti. Bugünkü hâl **fail-closed**'du (migration
> pod'u `ImagePullBackOff`'a düşer, `.status.failed` artmaz, anket 600 sn'yi harcar,
> `rollout`'a hiç varılmaz) ama benim raporumda *"önce Public yarat"* diye
> **kullanıcıya talimat** olarak duruyordu — kartın imza kusuru. Artık kapı: push'tan
> hemen sonra, `kubectl` kurulmadan **önce**, iki imaj da **kimliksiz** çekilebiliyor
> mu diye bakılıyor.
> **Anonimlik `docker logout` ile DEĞİL, `DOCKER_CONFIG`'i boş bir dizine çevirerek
> sağlandı** — ve gerekçesi ölçüm: bu makinede `~/.docker/config.json` →
> `credsStore: desktop`, yani `logout` bir *credential helper* üzerinden gider ve
> runner'daki düz dosya davranışını **temsil etmez**. **Ayırt edici kontrol:** boş
> `DOCKER_CONFIG` + `library/postgres:17-alpine` → **rc 0**; **uydurma** bir docker.io
> kimliği taşıyan `DOCKER_CONFIG` + aynı imaj → **rc 1, `unauthorized: incorrect
> username or password`**. İkincisi olmasa birincinin anonim olduğu kanıtlanamazdı.
> Kapının docker'a verdiği dizin doğrudan basıldı: `entries=[]`.
> **Dejenere yol sayımı — 10 hücre, YALNIZ BİRİ GEÇİYOR:** depo yok ×2 (bugünkü
> gerçek hedefler) → **rc 1, 2 hata** · public depo/etiket yok → **rc 1, 2** · registry
> ulaşılamıyor (DNS) → **rc 1, 2** · `docker` ikilisi yok (coreutils var) → **rc 1, 2**
> · **ikisi de çekilebiliyor → rc 0, 2 ok** · biri OK biri `denied` → **rc 1, 1 hata +
> 1 ok** (atıf doğru) · `429 toomanyrequests` → **rc 1, 2** · sessizce düşüyor → **rc
> 1, 2** · **çıkış 0 ama HİÇ çıktı yok → rc 1, 2** · çıkış 0 ama manifest olmayan
> çıktı → **rc 1, 2**. Yani **1 geçer, 9 fail-closed, 0 fail-open**.
> 🔴 **VE SONDAN İKİNCİ HÜCRE İLK TASLAKTA FAIL-OPEN'DI — kendi matrisim yakaladı.**
> Kapı yalnız çıkış koduna bakıyordu; sıfır dönüp **hiçbir şey basmayan** bir istemci
> *"anonymous pull OK"* diye geçiyordu: reponun adını koyduğu tuzağın en saf hâli
> (*sağlıklı cevap ile BOŞ cevap aynı şey değildir*). Kapı artık **kanıta** bakıyor —
> çıktı `"schemaVersion"` taşımak zorunda; üç public referansta ölçüldü
> (`postgres:17-alpine`, `alpine:3`, `hello-world:latest` → rc 0, 5000–6339 bayt, üçünde
> de var). Taşımazsa **fail-closed**.
> ⚠️ **Bu turda kendi ölçüm harnessimde iki hata yaptım, ikisi de deftere:**
> (a) ilk matriste *"her ikisi de PUBLIC"* diye etiketlediğim hücre aslında *"public
> depo, etiket yok"*tu — kapı `"$IMAGE:sha-$SHORT"` kuruyor ve hiçbir public deponun
> `sha-x` diye bir etiketi yok, yani **yanlış hücreyi ölçüp doğru sandım**; stub'la
> ayrıca koşuldu. (b) *"docker ikilisi yok"*u `PATH=/nonexistent` ile denedim ve
> `mktemp` de kayboldu, yani ölçtüğüm şey docker'ın yokluğu değil coreutils'in
> yokluğuydu (rc 127, çıktı yok); coreutils'i sembolik bağlarla koruyan bir PATH ile
> yeniden koşuldu → **rc 1, 2 adlandırılmış hata**.
> (c) 🔴 **VE ÜÇÜNCÜSÜ SINIFIN TAM ORTASINDAN: sarkan-atıf TARAYICIM FAIL-OPEN'DI.**
> Üç alternatifli bir regex yazıp `k = m.group(1) or m.group(2)` diye okuyordum ve
> `if not k: continue` ile devam ediyordum — yani **üçüncü** gruba düşen her eşleşme
> sessizce atlanıyordu. Tarayıcı *"sarkan atıf = 0"* diyordu; aynı dosyayı tek
> parça okuyan düzeltilmiş sürüm `deploy.yml`'in hata mesajındaki
> **ASCII'ye indirgenmiş** bir sınır anahtarını (`[Docker Hub anonim cekme butcesi]`,
> README'de `[Docker Hub anonim çekme bütçesi]`) **hemen** buldu — yani operatörün
> grep'leyip **hiç bulamayacağı** bir atıf. Anahtar mesajdan çıkarıldı (CLAUDE.md §7:
> hata mesajları ASCII İngilizce; bölüm **adıyla** gösteriliyor) ve `Dockerfile`'daki
> atıf README anahtarına **byte-byte eşit** olduğu doğrulandı. **Ders, bu turun kendi
> cümlesiyle: bir doğrulama aracı da bir kod yoludur ve dejenere girdisi vardır —
> benimkinin dejenere girdisi kendi regex'imin üçüncü grubuydu.**
>
> **U1 — `batch/jobs`'ta `watch` YOK, bugün doğru, ve artık YAZILI.** Denetçinin
> uyarısı kendi ölçümümle yeniden üretildi, iki dalın ikisi de: finalizer **yokken**
> (bugünkü hâl) `kubectl delete job --wait=true -v=8` → `DELETE`, sonra `GET .../jobs/
> tappa-migrate` → **404**, `watch=true` isteği **0**; kum havuzunda Job'a bir
> finalizer takıp aynı komut → `DELETE`, `GET` (nesne duruyor), sonra
> `GET .../jobs?fieldSelector=metadata.name%3Dtappa-migrate&…&watch=true`,
> `watch=true` isteği **1**. Canlı Job `{.metadata.finalizers}` → **boş**. Yani ikinci
> dal bugün oluşmuyor; oluşursa **Forbidden → adım kırmızı → migration'sız şemaya
> ikili sevk edilmez**, yani fail-closed. Kural yorumu bu ölçümle genişletildi;
> **fiil eklenmedi**, çünkü kimsenin görmediği bir vaka için verilen yetki kimsenin
> denetlemediği yetkidir.
>
> **U2 — `tr -d '[:space:]'` BAYT YÖNELİMLİYDİ, VE YEREL ÖLÇÜMÜM RUNNER'I TEMSİL
> ETMİYORDU.** Denetçi ölçtü: yalnız **U+00A0** (`c2 a0`) ya da **U+3000**
> (`e3 80 80`) içeren bir değer GNU coreutils'te (yani `ubuntu-latest`'te) kırpılmadan
> **geçiyor**. Bu makinede BSD `tr` ikisini de kırpıyor, yani **benim yerel ölçümüm
> tam tersini söylüyordu** — ve `gtr` bu makinede kurulu değil, yani runner'ın davranışı
> burada **hiç** ölçülemezdi. Sonucu ağır değildi (bir adım sonra `docker login`
> düşer, fail-closed) ama adımın **var olma sebebini** — eksik secret'ı **adıyla**
> söylemek — o girdide karşılamıyordu.
> **Çözüm aracı değiştirmek değil, testi ÇEVİRMEK oldu:** *"yalnız boşluk mu"* yerine
> *"kullanılabilir bir karakter içeriyor mu"* — `case $u in *[0-9A-Za-z]*)`. Harici
> araç yok, yani yerel/runner ayrımı da yok. Beş locale'de ölçüldü (`C`, `C.UTF-8`,
> `en_US.UTF-8`, **`tr_TR.UTF-8`**, `POSIX`): U+00A0 ve U+3000 **beşinde de REDDEDİLDİ**,
> `alice` beşinde de kabul; Türkçe locale'in noktasız-i tuzağı ayrıca sınandı
> (`ıstanbul9` → kabul) ve yalnız rakamdan oluşan `12345` → kabul. Geçerli hiçbir
> girdi elenmiyor: Docker Hub kullanıcı adı `[a-z0-9]`, PAT `dckr_pat_<base64url>`.
> **16 hücre koşuldu → 11 fail-closed (hepsi eksik secret'ı adıyla söylüyor), 5 geçiş**
> (hepsinde gerçek bir alfanümerik var), ve değer **hiçbir yolda echo edilmiyor**
> (`::error::PWNED-MARKER` girdisi → çıktıda **0** eşleşme; yalnız `${#u}` basılıyor).
>
> **🔴 [FAZ D · 2026-08-16] 3. TUR — `tappa-security-auditor` RED. İKİ BLOKLAYANIN
> İKİSİ DE §4.6'NIN ALTYAPI YÜZÜ, VE İKİSİ DE BU TURDA BENİM YAZDIĞIM METİNDEYDİ.**
> Kod değil, **runbook**. Ders keskin: *"ürün temiz"* ile *"runbook operatörü doğru
> yere gönderiyor"* ayrı sorulardır ve ikincisi de bloklayabilir.
>
> **B1 — REÇETE EDİLEN EYLEM GARANTİLİ BAŞARISIZDI.** Baştan yazdığım *"Olay
> müdahalesi §2"*, üç satırlık tablodan sonra **"Çare (her üçünde de) yeni bir deploy
> koşusudur"* diyordu — ve bu, tablonun **kendi çare sütunuyla iki satırda
> çelişiyordu**. Ölçüldü: depo private iken yeni koşu, bu turda **benim eklediğim
> kapıda** ölür — kapı `deploy.yml`'de **10. adım**, `Install kubectl` **11. adım**,
> yani `kubectl` runner'a daha kurulmamıştır; kapı rc 1 verir ve *"Nothing has been
> applied to the cluster"* der. Yani operatörün yaptığı tek şey kümeye dokunmadan
> durmak olur ve **uygulama pod'u açılmamaya devam eder**. `429` satırında da
> geçersiz: kapı runner'ın adresinden geçse bile çeken taraf **düğüm**dür.
> 🔴 **Neden §4.6:** bu bölümün müdahale ettiği hâl pod'un **hiç açılmadığı** hâldir —
> 04:00 vardiyası tap sayfasını yükleyemez ve **kaydedilemeyen dokunuş kaybolan
> kayıttır**. Genel cümle **kaldırıldı**; tablo başlığı artık *"satır bazında; genel
> bir çare YOK"* diyor ve her satır kendi doğru sırasını taşıyor (*"önce Public,
> SONRA yeni koşu"* · *"bekle, yeni koşu düzeltmez"* · *"yeni koşu"*).
>
> **B2 — AYNI DOSYA KENDİNİ YALANLIYORDU.** Birinci satırın yanına *"bunu kümede
> görüyorsan iş akışının kapısı atlanmıştır … buraya ancak **elle `apply` edilmiş**
> bir manifestle gelinir"* yazmıştım. **Yanlış**, ve çürüten cümle **aynı dosyanın
> sınırının **[Docker Hub anonim çekme bütçesi]** maddesinde** duruyordu (*"kapı bir depoyu sonradan private'a çeviren kimseyi
> engelleyemez"*). Elle apply olmadan dört adımda üretilir ve **dördü de sınır **[Docker Hub anonim çekme bütçesi]**'nde
> sayılı**: deploy N yeşil → depo sonradan private → düğüm imajı düşürür
> (`imageMinimumGCAge: 2m0s`, `crictl rmi`, düğüm yeniden kurulumu) → pod yeniden
> başlar. Operatör **hiç olmamış** bir olayı ararken pod kapalı kalırdı. Cümle
> *"kapı bir **deploy anı** ölçümüdür, süregelen bir garanti değil"* ile değiştirildi.
>
> **U1 — 18. SINIR: sevk edilen commit'in kimliği herkese açık (§4.7).** Liste 1..17
> ardışıktı ama **hiçbir maddesi ürün ikililerinin herkese açık olmasını
> adlandırmıyordu**; 12. madde yalnız çekme bütçesini sayıyordu. Denetçinin ölçtüğü
> gibi bedelin **büyük kısmı sıfır** ve bunu kendi ölçümümle doğruladım: depo zaten
> **PUBLIC** (`gh repo view` → `PUBLIC`), imaj içerikleri `Dockerfile`'ın son iki
> aşamasından birebir okunuyor — uygulama = CA paketi + `/tappa`; migration = CA
> paketi + `/goose` + `/migrations` (**20 `.sql`**, `find db/migrations -name '*.sql'`
> ile sayıldı, hepsi zaten public depoda). 🔴 **Ama sayılmayan gerçek bir delta var
> ve kontrol ölçümüyle doğrulandı:** public bir Docker Hub deposunun etiket listesi
> **kimliksiz okunuyor** (`library/alpine`: anonim token → `/v2/…/tags/list`
> **HTTP 200**; `hub.docker.com/v2/repositories/library/alpine/tags` → etiket adları
> düz döndü). Etiketler `sha-<12hex>` olduğu için **üretimdeki commit ve bir
> düzeltmenin sevk edilip edilmediği dışarıdan okunabilir**. Bunu başka hiçbir yüzey
> vermiyor — ölçüldü: `grep -rn buildinfo internal/handler web/templates` → **0**;
> `buildinfo` yalnız `cmd/tappa/main.go:83`'te log'a yazılıyor. Yani M8-01'in
> *"derleme kimliği herkese açık bir uç noktaya çıkmıyor"* kararı **yeni bir
> kanaldan** deliniyor. 18. madde yazıldı (ne açılıyor · ne açılmıyor, ölçülmüş dosya
> listesiyle · neden kabul edildi · kapatmanın bedeli). Ayrıca operatör adımı 4'ün
> *"imaj `scratch` + iki dosya"* cümlesi **iki artefaktın yalnız birini** anlatıyordu;
> düzeltildi.
>
> **U2 — DOSYADAKİ SON SATIR-İÇİ SIR GENİŞLETMESİ KALKTI (§4.7).**
> `echo "${{ secrets.KUBE_CONFIG }}" | base64 -d` — bir `run:` gövdesindeki ifade
> kabuk başlamadan **önce** yerine konur, yani runner diskindeki render edilmiş adım
> betiğinde **düz metin** durur. Bu, dosyanın **en değerli** kimliğiydi (sınır 8'e
> göre hâlâ cluster-admin olabilir) ve bu turda **benim eklediğim iki adım** zaten
> doğrusunu yapıyordu. Artık `env: KUBE_CONFIG_B64` + `printf '%s' "$KUBE_CONFIG_B64"`
> (`echo` değil: başında `-` olan ya da ters bölü taşıyan bir değerde davranışı
> kabuğa bağlı). Ölçüm: `grep -n 'secrets\.'` → **5 eşleşme, beşi de `env:`
> bloğunda, `run:` gövdesinde 0**. Sızmadığı da ölçüldü: her `run:` gövdesi
> ayrıştırılıp yorumlar atılarak → **17 gövde, yorum-dışı `set -x` sayısı 0**.
> ⚠️ Ve bozuk bir secret'ın nerede yakalandığı ölçüldü: `base64 -d` **hoşgörülü**
> (boş değer → 0 baytlık dosya, exit 0; base64 olmayan değer → 6 bayt çöp, exit 0);
> yakalayan şey adımın **son satırı**: `kubectl config current-context` → ikisinde de
> **exit 1**, kümeye hiçbir istek gitmeden.
>
> **🔴 BU TURDA KENDİ DOĞRULAMA ARAÇLARIM İKİ KEZ DAHA YANLIŞ SAYDI — sınıfın
> yedinci ve sekizinci örneği, ikisi de kendi ölçüm harness'imde.**
> (a) *"secrets satır-içi mi"* sınıflandırıcım `[A-Z_]+` yazıyordu ve **yeni değişken
> `KUBE_CONFIG_B64` rakam içerdiği için** `env:` bloğundaki satırı *"INLINE"* saydı —
> yani düzelttiğim şeyi düzeltilmemiş gösterdi. `[A-Z0-9_]+` ile yeniden koşuldu:
> **5 env: / 0 inline**. (b) *"`set -x` bu dosyada hiç geçmiyor"* diye yazdığım
> cümleyi `grep -c 'set -x'` **1** ile yalanladı — tek eşleşme **cümlenin kendisiydi**.
> İddia ölçülebilir hâle çevrildi: yorumlar atılarak, `run:` gövdelerinde **0**.
> **Ders, 2. turun (c) maddesinin devamı: bir doğrulama aracı da bir kod yoludur; ve
> bir iddiayı ölçülebilir yazmazsan, kendi metnin onu yalanlayabilir.**
>
> **🔴 [FAZ D · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED, ÜÇ BLOKLAYAN. VE BULGU
> SINIFIN DOKUZUNCU ÖRNEĞİ, YENİ BİR YERDE: AĞACI ÖLÇTÜM, RUNBOOK'UN KONUSU OLAN
> KÜMEYİ HİÇ ÖLÇMEDİM.** Dört tur boyunca `git diff`, `bash -n`, `--dry-run=client`
> ve stub matrisleri koşturdum; **`kubectl -n tappa get deployment -o jsonpath` bir
> kez bile koşmadı**. Runbook'un doğruluğu ağaca değil **kümeye** göredir.
>
> **KÜMENİN BUGÜNKÜ HÂLİ (kendi ölçümüm, salt okuma, 2026-08-16):**
> `secret/ghcr` **VAR** (18 sa, `dockerconfigjson`) · `deployment/tappa` →
> `ghcr.io/atknatk/tappa:sha-353897c6d5f6`, `imagePullSecrets: ghcr` · **üç
> ReplicaSet'in üçü de** `ghcr.io/...` + `ps=ghcr` · Job → `ghcr.io/atknatk/
> tappa-migrate:...` + `ps=ghcr` · `gh secret list` → **yalnız `KUBE_CONFIG`** ·
> `hub.docker.com/v2/repositories/atknatk/{tappa,tappa-migrate}/` → **404, 404**.
> Yani **ağaç Docker Hub'ı tarif ediyor, küme GHCR koşuyor**, ve operatör adımı 4
> yapılmadan hiçbir Docker Hub deploy'u koşamaz (1. adımda durur). ✅ Yan bulgu:
> FAZ C **inmiş** — Deployment ve Job'ın ikisi de `wait-for-postgres` taşıyor.
>
> **B1 — RUNBOOK BİR DURUMU ŞİMDİKİ ZAMANDA ANLATIYORDU VE BİR KOMUTU ÜRÜNÜ
> DÜŞÜRÜRDÜ.** *"Elle deploy / rollback"*taki
> `set image ... tappa=docker.io/atknatk/tappa:sha-<12hex>` bugün koşulsa **çekilemeyen
> bir imajı** `1/1 Running` bir pod'un yerine koyardı → `ImagePullBackOff`. **3. turun
> RED'i *garantili başarısız* içindi; bu *garantili zararlı*.** Ve aynı kökten dört
> yanlış cümle daha: *"`rollout undo` artık ayrıcalıklı risk taşımıyor"* (üç RS de
> `ghcr` + `ps=ghcr` → tam da kaldırdığım 401 senaryosu) · *"'ayağa kalkabilir miyim'
> sorusu ARTIK YOK"* (oluşabilir, ve onu ölçen `pull-credential` **silindi**) ·
> *"`secret/ghcr` arayan biri var olmayan bir nesneyi arar"* (**var**) · sınır **[Docker Hub anonim çekme bütçesi]**'nin
> *"kaldırıldı"*sı (ağaçta evet, kümede hayır).
> 🔴 **Ve dosya doğru kalıbı ZATEN BİLİYORDU:** bölüm 1'de *"Ağaçtaki manifestler
> taşıyor; KÜMEDEKİ nesneler henüz taşımıyor"* + doğrulama komutu duruyor. Aynı kalıp
> şimdi **dört yere daha** kondu: rollback bölümünün başına (ön koşul + ölçüm komutu,
> ve komuttaki registry `<registry>` ile parametrik hâle geldi), olay önsözüne, bölüm
> 2'ye ve sınır **[Docker Hub anonim çekme bütçesi]**'ne. `rollout undo` uyarısı **geri getirildi** ve hangi koşulda
> düşeceği yazıldı.
>
> **B2 — BÖLÜM 2'NİN BAŞLIĞI KENDİ BÖLÜMÜNÜ YALANLIYORDU.** *"artık kimlik sorunu
> DEĞİL, **iki başka sebep**"* — tablo **üç** satır taşıyordu ve bölümün kendi metni
> *"üç satırda da"* diyordu; üstelik kimlik sorunu bu kümede **hâlâ canlı**. Başlık
> *"dört sebep, ve hangisinin geçerli olduğu KÜMEYE bağlı"* oldu ve **dördüncü satır**
> eklendi (bugünkü GHCR 401'i, 2026-08-15'te **4 dk 39 sn** olarak ölçülmüştü).
> 🔴 **Kendi hatam, sınıfın onuncu örneği:** 9. iddiamda *"1 başlık, 1 ayraç, 3 veri
> satırı"* diye **doğru** saydım — ama sayımı **hiç başlığın kendisiyle
> karşılaştırmadım**. Tarayıcı bir önceki kusurun şekline (fazladan başlık satırı)
> göre yazılmıştı; o turun ürettiği şekle (başlıktaki sayı sözcüğü) göre değil.
>
> **B3 — SEVK EDİLEN BİR YORUMDA *ÖLÇÜM OLARAK DURAN* YANLIŞ BİR OLGU, VE ÖLÇÜM YİNE
> YEREL ARAÇLA YAPILMIŞTI.** `deploy.yml`'in kubeconfig adımı *"`base64 -d` lenient,
> gerçek kapı son satır"* diyordu. **Runner'da ölçüldü** (`docker run --rm
> ubuntu:24.04`, GNU coreutils **9.4**) ve yerelle yan yana kondu:
>
> | girdi | ubuntu:24.04 (GNU 9.4) | yerel BSD |
> |---|---|---|
> | boş | rc 0, 0 bayt | rc 0, 0 bayt |
> | base64 olmayan | **rc 1, `base64: invalid input`** | rc 0, 6 bayt |
> | yalnız boşluk | **rc 1** | rc 0, 0 bayt |
> | `-` ile başlayan | **rc 1** | rc 0, 3 bayt |
> | geçerli | rc 0, 15 bayt | rc 0, 15 bayt |
>
> Yani runner'da `base64 -d` **hoşgörülü değil**, *"6 bayt çöp"* **oluşmuyor**, ve son
> satır dört bozuk girdiden **yalnız birinin** (boş) kapısı. Yön hâlâ fail-closed, yani
> güvenlik değil **doğruluk ve teşhis** kusuru: yorum bir bakımcıya *"son satır
> gereksiz, silinebilir"* diye **yanlış bir nedensellik modeli** veriyordu — oysa boş
> girdiyi yakalayan **yalnız o satır**. 🔴 **Ağırlığı yerinde: bu, aynı dosyanın 118-128.
> satırlarında ADIYLA anılan kusurun (`tr -d '[:space:]'`, yerel-aracı-runner-aracı
> yerine koymak) AYNI TURDA, üç paragraf aşağıda tekrarı.** §4.7 temiz: GNU `base64`
> yalnız `base64: invalid input` basıyor, değeri basmıyor (markör sondası → **0**).
>
> **U1** `429` satırı Deployment pod'u için doğruydu ama **migration Job'ını
> kurtarmıyor**: `activeDeadlineSeconds: 600` + `backoffLimit: 2`, saatlik pencereden
> çok önce kalıcı `Failed` → yine yeni bir koşu gerekir. Satıra yazıldı.
> **U2** *"SADECE `ImagePullBackOff` olanı sil"* **pod tipini ayırmıyordu**; üç satırlık
> bir tablo eklendi: Deployment pod'u **zararsız** · **Job pod'u** `.status.failed`'ı
> artırır ve `backoffLimit: 2` ile **üç silme kalıcı `Failed`** yapar ·
> **`tappa-postgres-0`** *veritabanını yeniden başlatır*.
> **U3** olay önsözünün *"her satır bu kümede fiilen ölçüldü"* garantisi, altına
> **ölçülmemiş** Docker Hub satırları eklendiği için fazla iddia hâline gelmişti —
> cümle daraltıldı ve ölçülmemiş satırlar **satırında işaretlendi** (B1'le aynı kök:
> değişmemiş bir garanti cümlesinin altına yeni satır eklemek).
> **U4** *"`deploy.yml`'in on beş küme çağrısı"* yanlıştı: **15** benim prova betiğimin
> **adım numarasıydı**. Yeniden sayıldı, yöntemi yazıldı (yorumlar atılarak, API'ye
> giden `kubectl` çağrıları; `version --client`, `config current-context` ve
> `--dry-run=client` hariç, `db-role`'un `exec`'i dahil): **19** — adım 14→2 · 15→5 ·
> 16→5 · 17→3 · 18→1 · 19→3, denetçinin sayısıyla **birebir**.
> **U5** `dnscheck` sondası `postgres:17-alpine` istiyor, yani `429` olayında ters
> tepebilir; bir cümlelik uyarı eklendi.
>
> **🔴 [FAZ D · 2026-08-16] 7. TUR — YENİ ÜÇÜNCÜ GÖZ RED, TEK BLOKLAYAN, SINIFIN
> ON BİRİNCİ ÖRNEĞİ: DÜZELTMEYİ README'YE UYGULADIM, AYNI TURDA YENİDEN YAZDIĞIM
> DİĞER ÜÇ DOSYAYA UYGULAMADIM.** 5. turda kümeyi ölçüp README'yi gerçeğe eşitledim;
> ama `20-app.yaml`, `30-migrate-job.yaml` ve `deploy.yml`'de **aynı commit'te
> eklediğim** dört cümle, README'nin **adıyla geri çektiği** cümlelerdi. Repo aynı
> commit'te hem geri çekmeyi hem geri çekilen cümlenin iki kopyasını taşıyordu, ve
> bağ kopmuştu: sildiğim eski yorum *"deploy/README.md carries the warning in BOTH
> places"* diye README'ye işaret ediyordu, yenisinde o işaret **yoktu** — oysa README
> sınır 5 ve 12 okuru açıkça `20-app.yaml`'a **gönderiyor**. Olay anında oraya bakan
> operatör *"undo artık 401 almıyor"* okuyup `rollout undo` koşar ve **şu an
> `1/1 Running` olan üretim pod'unu** düşürürdü.
>
> 🔴 **VE ÇARE ÜÇ DOSYAYA DAHA UYARI EKLEMEK DEĞİLDİ — İDDİANIN BİÇİMİYDİ.** Sınıf on
> bir kez tekrarladı ve her seferinde çare *"bir yer daha düzelt"* oldu; FAZ C'yi kıran
> şey buydu ve burada da o geçerli: **savunulan bir cümleyi, bayatlayamayacak bir
> cümleyle değiştir.** *"The repository **is** public"* dünya hakkında bir iddiadır ve
> bayatlar; *"**WHILE** the image was pulled with a credential, X; **ONCE** it is
> pulled anonymously, Y"* mekanizma hakkındadır ve bayatlayamaz. Üç dosyadaki dört
> cümle **koşullu** hâle getirildi, hepsi *"bugünkü durumun tek kaydı
> `deploy/README.md` → Elle deploy / rollback"* diye **tek yere** işaret ediyor (eski
> yorumun yaptığı gibi). ⚠️ **Ve bu iddia gerçeğe indiriliyor:** *"bir sonraki geçişte
> tek bir yer güncellenecek"* fazla iddiaydı — ölçüldü, bugünkü küme durumu
> `deploy/README.md` içinde **altı ayrı bölgede** tekrarlıyor. Doğru cümle: **tek
> DOSYA, altı bölge, ve altısının da kendi düşme koşulu yazılı** — yani hiçbiri
> sessizce bayatlayamaz.
> **Ölçüm — istenen hedef ve sonucu:** `deploy/` + `.github/` altındaki *bugünkü küme
> hakkında şimdiki-zaman iddiası* sayısı → **0**. Kalan dört eşleşmenin hepsi elle
> tasnif edildi: 2 **koşullu** (WHILE/ONCE) · 1 **bu dosya hakkında** (*"a pod born
> from THIS spec"*) · 1 **genel mekanizma** (*"a public repository … by definition"*).
>
> **🔴 VE DETEKTÖRÜM İKİ KEZ YANILDI — sınıfın on ikinci ve on üçüncü örneği, ikisi de
> benim doğrulama aracımda, ikisi de kendi matrisimde yakalandı.**
> (a) İlk sürüm niteleyiciyi **±2 satırlık komşulukta** aradı; `30-migrate-job.yaml`'da
> üç satır ötedeki alakasız bir *"used to"*, çıplak bir dünya iddiasını **temize
> çıkardı** — yani detektör *"0 çıplak iddia"* dedi, oysa vardı. Cümle kapsamına
> daraltıldı ve iddia gerçekten koşullu yazıldı.
> (b) İkinci sürüm cümleyi `;` ve `:`'de de böldü, böylece *"ONCE …:"* koşulu
> **kendi sonucundan koparıldı** ve yönetilen bir yan cümle **çıplak** göründü. Tam
> nokta ile bölmeye çevrildi. **Ders (2. ve 3. turun devamı): bir detektörün kapsam
> penceresi de bir dejenere girdidir.**
>
> **VE 5. TURUN İKİ KENDİ HATASI DEFTERE GEÇMEMİŞTİ — buraya yazılıyor** (denetçi
> `grep` ile gösterdi; bu repoda kayıt sohbette değil defterde yaşar):
> (a) bölüm 2'nin satır sayımını ilk yeniden koşuşumda seçici **iki tabloyu birden**
> süpürdü ve `DATA ROWS: 7` dedi (doğrusu: sebep tablosu **4**, pod-tipi tablosu
> **3**); (b) *"canlı prose iddiası"* regex'im `**Dört satırda`yı **büyük harf**
> yüzünden kaçırdı, sonra büyük/küçük harfe duyarsız koşuldu.
> ✅ **VE SINIFI KIRAN ARAÇ DA DEFTERE YAZILIYOR, çünkü kayıtsız bir araç yoktur:**
> 5. turda yazılan **başlık↔tablo kontrolü** — bir başlıktaki sayı sözcüğünü (*"dört
> sebep"*) başlığın yönettiği tablonun **gerçek satır sayısıyla** karşılaştırır,
> bölümdeki her tabloyu ayrı sayar ve düzyazıdaki sayı iddialarını da tarar. Bağımsız
> denetçi onu yeniden koşturdu: **6 tablo**, *"dört sebep"*↔4 · *"Dört satırda"*↔4 ·
> *"üç tipin üçü"*↔3 · *"Üç ayrı taze koşu"*↔3, iki yanlış pozitif (artikel *"bir"*),
> **gerçek uyumsuzluk 0**. Bu kontrol, 4. turda *"1 başlık, 1 ayraç, 3 veri satırı"*
> diye **doğru** sayıp **hiç başlıkla karşılaştırmadığım** kusurun yerine geçiyor.
>
> **N1** `kubectl set image` yer tutucuyu **sessizce kabul ediyor** — çevrimdışı
> ölçüldü (`--local`): çıktı birebir `<registry>/atknatk/tappa:sha-<12hex>`, istemci
> tarafında imaj doğrulaması **yok**. Komut bloğuna yazıldı. **N2** `rollout undo`
> uyarısı kod bloğunun **ardındaydı** ve bloğu toptan kopyalayan onu okumadan koşardı;
> blok **öncesine** bir uyarı ve **üçüncü komutun üstüne** bir işaret kondu. **N4** bir
> sınır anahtarı satır sonunda **bölünmüştü** (`**[Docker Hub anonim çekme\n>
> bütçesi]**`) — render doğru, **arama** değil: altı görünümünün yalnız dördü literal
> `grep`'le bulunuyordu; ikisi birleştirildi → **6/6**. 2. turun ASCII-katlanmış
> anahtar bulgusuyla aynı sınıf, farklı mekanizma. **N5** *"İki tanesi ölçüldü, biri
> yapısal"* **üç** sebep vaat edip **iki** veriyordu → *"İki tane, biri ölçülmüş biri
> yapısal"*.
> ⚠️ Denetçinin eklediği eksik de yazıldı: GNU `base64` hata vermeden **2 bayt kısmi
> çıktı** yazıyor, yani `config` yarım yazılıp adım kırmızıya düşüyor — yön
> fail-closed, sonucu değiştirmiyor.
>
> **🔴 [FAZ D · 2026-08-16] 9. TUR — SON. ÜÇÜNCÜ GÖZ RED, TEK BLOKLAYAN, VE SINIFIN
> ON DÖRDÜNCÜ ÖRNEĞİ: KAPSAMI YİNE BEN SEÇMİŞTİM.**
> Mekanizma her seferinde aynı: **taramayı bir önceki kusurun bulunduğu yere göre
> kapsamlıyorum.** 5. tur: README'yi ölçtüm, **kümeyi** ölçmedim. 7. tur: README'yi
> düzelttim, **aynı turda yeniden yazdığım üç manifesti** düzeltmedim. 9. tur: üç
> manifesti düzelttim, **`scripts/` ve `Dockerfile`'ı** taramadım — üstelik
> `verify-deployment.sh` bu turda **127 satır** kaybetmiş, yani aynı commit'te
> yeniden yazılmış bir dosya.
>
> **BLOKLAYAN — `scripts/verify-deployment.sh:13-18`.** `pull-credential` modunun
> silinme gerekçesi **koşulsuz şimdiki zamanda** yazılmıştı: *"There is no
> `secret/ghcr` any more … nothing records, expires or overwrites a pull credential …
> a diagnostic that says something alarming about a condition that **cannot occur**"*.
> Üç cümlenin üçü de yanlış ve *"cannot occur"* dediği koşul **canlı** (bugün yeniden
> ölçüldü: `secret/ghcr` **var** · `deployment/tappa` → `ghcr.io/…` + `ps=ghcr` ·
> Docker Hub **404, 404**). Kaldırılan teşhisi arayan kişi, **aracın kendi dosyasında**
> *"bu koşul oluşamaz"* okuyacaktı — ve tarif ettiği arıza pod'un **hiç açılamaması**
> (§4.6). Blok WHILE/ONCE biçimine çevrildi ve *"WHILE bir küme hâlâ kimlikle
> çekiyorsa koşul canlıdır; elle okunacak iki komut README'de"* bilgisi eklendi.
>
> **BEŞ DOSYADA ALTI CÜMLE DAHA AYNI BİÇİME ÇEVRİLDİ:** `Dockerfile:11` (*"THE
> REGISTRY MOVED … TO A PUBLIC …"* → *"bu depo Docker Hub'a push ediyor; WHILE
> private … ONCE public …"*) · `deploy.yml:43` (*"The images live on Docker Hub now"*
> → *"this workflow pushes to Docker Hub … a property of THIS FILE"*) ·
> `deploy.yml:475` (*"With the images on a public … there is no pull credential at
> all"* → *"this workflow no longer writes one; whether one still exists in a cluster
> is not a question this file answers"*) · `deploy.yml:295` (*"NEITHER REPOSITORY
> EXISTS"* → **tarihli snapshot** + *"ilk başarılı push'ta bayatlar; adım buna
> dayanmıyor"*) · `01-rbac.yaml:38` (*"is gone with the move to a public …"* → *"gone
> from THIS REPOSITORY … bir küme hâlâ o Secret'ı tutuyor olabilir"*) ·
> `20-app.yaml:135` (*"The image is downloadable by anyone"* → *"ONCE the repository
> is public …"*).
>
> **KAPSAM BU KEZ SEÇİLMEDİ: `git status --short`'un TAMAMI tarandı** (on dosya,
> orkestratörün `backlog.md`/`state.md`'si dahil — okundu, **değiştirilmedi**).
> **76 aday cümle**, dosya dosya: `deploy.yml` 9 · `Dockerfile` 4 · `README` 10 ·
> `20-app.yaml` 8 · `30-migrate-job.yaml` 5 · `01-rbac.yaml` 4 ·
> `verify-deployment.sh` 4 · `backlog.md` 1 · kart 19 · `state.md` 12. Tasnif:
> **15 koşullu · 9 bu-dosya-hakkında · 32 tarihli defter kaydı · 20 elle okundu**.
> Yirmisinin yirmisi de ya bu dosya/derleme hakkında (Dockerfile'ın `CGO_ENABLED=0`'ı,
> `01-rbac.yaml`'ın *"this identity can no longer …"*'ı), ya geri çekme alıntısı, ya
> genel mekanizma, ya da regex'in yakaladığı YAML/kod bloğu. **Bugünkü küme hakkında
> kalan şimdiki-zaman iddiası: 0.**
>
> **C6 — İDDİA GERÇEĞE İNDİRİLDİ.** `20-app.yaml`'ın *"a later transition needs one
> edit, not four"*'ı ve kartın *"tek bir yer güncellenecek"*i fazla iddiaydı: ölçüldü,
> bugünkü küme durumu `deploy/README.md` içinde **altı ayrı bölgede** tekrarlıyor.
> Doğrusu **tek DOSYA, altı bölge** — ve altısının da **kendi düşme koşulu yazılı**,
> yani hiçbiri sessizce bayatlayamaz. İkisi de düzeltildi.
>
> **🔴 VE BU TURUN KENDİ HATASI, GÜNÜN ÜÇÜNCÜSÜ AYNI SEBEPTEN:** tasnif edicim yine
> **büyük/küçük harfe duyarlıydı** — *"If this says …"* ve *"This workflow no longer
> …"* yalnızca baş harfleri büyük olduğu için *"sınıflandırılamadı"* kovasına düştü.
> Aynı sebep bugün üç kez ısırdı (5. turda `**Dört satırda`, 7. turda `grep -i`'nin
> Türkçe `İ`'yi katlamaması, şimdi bu). `IGNORECASE` ile yeniden koşuldu ve **kalan
> her madde elle okundu** — regex hükmüne değil.
>
> **AÇIK KALAN, DOĞRULANAMADI DİYE YAZILIYOR:** `KUBE_CONFIG` secret'ının **içindeki
> kimlik** okunamadı (ajanın secret okuma yolu yok). Hâlâ cluster-admin bir kubeconfig
> ise `01-rbac.yaml`'daki daraltma **hiçbir şeyi sınırlamaz** — cluster-admin RBAC'e
> bakmaz. README sınır **[kubeconfig'in kimliği doğrulanmadı]**, ve operatör adımı 5
> onu kapatan komutu taşıyor. Ayrıca `pods/exec`'in `resourceNames` daraltması **canlı
> `ns/tappa`'da koşturulmadı** (salt-okuma sınırı) — kum havuzunda birebir kurulumla
> ölçüldü.
>
> ---
>
> **KART DÜZELTMESİ (2026-08-16, FAZ E — "yedek + PROVALI geri yükleme").
> KRİTER *"Yedek: otomatik, geri yükleme DENENMİŞ"* AĞAÇTA KARŞILANDI, KÜMEDE
> OPERATÖRÜ BEKLİYOR — VE PROVA KARTIN HİÇ ÖNGÖRMEDİĞİ BİR KUSURU BULDU.**
> Değişen dosyalar: **yeni** `deploy/k8s/50-backup.yaml` · **yeni**
> `scripts/pg-backup.sh` · **yeni** `scripts/pg-backup-ship.sh` · **yeni**
> `scripts/pg-restore-verify.sh` · `.github/workflows/deploy.yml` (tek adım, +17
> satır) · `deploy/README.md`. **Go kodu değişmedi** (`git status --short` → **0**
> adet `.go`), `ci.yml` **değişmedi** (`git diff --stat` → boş). **Kümeye hiçbir
> mutasyon uygulanmadı** — kum havuzu bile açılmadı (`kubectl get ns tappa-verify` →
> boş); bütün prova **Docker'da**, ayrı konteynerlerde koştu ve konteynerler silindi.
> Canlı ürün dört ayrı anda ölçüldü: `/healthz` **200**, `/readyz` **200**.
>
> **🔴 KARTIN TUZAK #1'İ (RLS'in sessiz boş yedeği) GERÇEK ÇIKTI AMA MEKANİZMASI
> BRIEF'İN TARİF ETTİĞİNDEN FARKLI — VE FARK ÖNEMLİ.** Brief *"`app.tenant_id` set
> edilmemiş bir bağlantıyla alınan dump sıfır satır üretebilir ve `pg_dump` exit 0
> verir"* diyordu. Ölçüm (bu reponun kendi veritabanı, 231 832 `transactions` satırı):
>
> | komut | sonuç |
> |---|---|
> | `psql` `tappa_owner` (rolsuper=t, rolbypassrls=t) | 231832 satır |
> | `psql` `tappa_app` (f/f), GUC yok | **0 satır** — tuzağın önkoşulu **doğru** |
> | `pg_dump -U tappa_app --data-only -t transactions` | **exit 1**, `ERROR: query would be affected by row-level security policy` — **FAIL-CLOSED** |
> | ...`--enable-row-security` ile | **exit 0**, 966 bayt, **0 satır** — *sessizce boş yedek* |
> | ...GUC bir tenant'a set edilmişken | **exit 0**, 231832'nin **17 262'si** — *sessizce KISMİ yedek* |
>
> Yani `pg_dump` **varsayılan olarak** `row_security = off` kuruyor ve bypass edemeyen
> rol **hata alıyor**; tehlikeli artefakt **tek bir bayrağın** arkasında. Ve üçüncü
> satır brief'te hiç yoktu: kısmi bir yedek, boş olandan **daha** tehlikeli, çünkü
> boyutu makul görünür. `pg-backup.sh` üç katmanda kapatıyor, ikisi **yapısal**:
> bayrak hiç geçilmiyor · rol `rolsuper`/`rolbypassrls` değilse iş `pg_dump`'a **hiç
> ulaşmadan** duruyor (ölçüldü: `tappa_app` ile exit 1 ve adıyla mesaj) · ve bitmiş
> dump'ın satırları canlıyla karşılaştırılıp *"canlıda dolu, dump'ta boş"* olan her
> tablo işi kırmızıya çekiyor.
>
> **🔴 VE PROVANIN BULDUĞU, KARTTA DA BRIEF'TE DE OLMAYAN KUSUR: DÜZ BİR GERİ YÜKLEME
> `tappa_app`'E 31 FAZLA YETKİ VERİYOR — `transactions` ÜZERİNDE **UPDATE VE DELETE
> DAHİL** (§4.3).** Brief'in uyarısı *"geri yüklenmiş bir veritabanı RLS'siz ya da
> GRANT'sız doğabilir"* idi; ölçüm **tam tersini** buldu — GRANT'sız değil,
> **GRANT-FAZLASI**. Sayılar: kaynak **45** tablo yetkisi (sahip hariç), taze bir
> pod'a geri yükleme **76**. Fazlalar arasında `audit_log` DELETE, `admin_users`
> INSERT/UPDATE, `legal_documents` tam CRUD.
> **Mekanizma:** `01-roles.sql` initdb'de `ALTER DEFAULT PRIVILEGES … TO tappa_app`
> koşuyor → geri yüklemedeki her `CREATE TABLE` dördünü de otomatik veriyor →
> `pg_dump` ACL'i **yerleşik** varsayılana göre hesapladığı için fazlalığı geri alan
> `REVOKE`'ları hiç yazmıyor. Dump doğru; hedef boş değil.
> ⚠️ **VE ÜRÜN BOZULMUYOR — kusurun görünmez olmasının sebebi bu.**
> `transactions_no_mutation` tetikleyicisi UPDATE'i **yine** reddediyor. Elle bakan
> biri *"satırı değiştirebiliyor muyum? hayır"* deyip temiz sanar. Fark **yalnız hata
> kodunda**: doğru geri yüklemede `permission denied for table transactions`
> (**42501**), bozukta `append-only table transactions` (**P0001**).
> **Çözüm ölçülerek seçildi, ve seçilen şey bir yama değil bir PROSEDÜR:** geri
> yükleme artık **aynı instance'ta yeni bir veritabanına** yapılıyor — çünkü
> `pg_default_acl` **veritabanı başına** bir katalog (ölçüldü: `tappa`'da 2 satır,
> yeni veritabanında **0**, geri yükleme sonrası dump'ın kendi son iki satırıyla yine
> **2**, yani sonraki migration'lar da doğru davranıyor). Taze pod'a mecbur kalanlar
> için iki satırlık askıya alma adımı yazıldı ve **ölçüldü** (76 → **45**).
>
> **PROVA — ÜÇ KONTROLLÜ KOŞU, BİRİ KASITLI NEGATİF.** Gerçek bir yedekle
> (19 tablo, **2 118 896** satır, 546 MiB düz / **120 MiB** gzip):
>
> | Koşu | Yetki | Doğrulama aracı |
> |---|---|---|
> | taze pod, düzeltme **yok** (negatif kontrol) | **76** (31 fazla) | **exit 1**, 31'ini adıyla listeledi |
> | taze pod, düzeltme **var** | **45** (fazla 0, eksik 0) | **exit 0** |
> | aynı instance'ta yeni DB + `RENAME` takası | **45** | **exit 0** |
>
> Üçünde de: `psql` **exit 0**, stderr **0 satır**, 19/19 tablo, satır sayıları
> manifest ile **birebir**, 18 politika / 18 `FORCE RLS` / 9 tetikleyici / 56 indeks /
> 98 fonksiyon / `goose 20` — hepsi kaynakla aynı. Ve **§4.5 geri yüklemeden sağ
> çıkıyor**: `tappa_app` olarak **TCP üzerinden** (loopback değil — bu reponun
> `01-roles.sql`'inin kendi dersi), GUC'suz **0** satır, GUC'lu **17 262** satır.
> Kaybolan tek şey §4.3'ün **yetki kemeriydi** ve onu yalnız hata kodunu assert eden
> bir kontrol gördü.
> **Süreler:** `pg_dump` 85 sn · doğrulama + gzip ~3 dk · toplam **4 dk 33 sn** ·
> `psql` geri yükleme **37–39 sn** · doğrulama aracı **23 sn** · iki
> `ALTER DATABASE RENAME` **399 ms** · boş bir şemanın yedeği **< 1 sn**.
>
> **DEJENERE GİRDİLER — KENDİ LİSTEM, KOŞTURULDU.** `pg-backup.sh` dokuz yol
> (**boş ama sağlıklı veritabanı → exit 0** · `tappa_app` ile → rol kapısı · **tablosu
> olmayan** veritabanı → adıyla ret · ulaşılamayan host · yanlış parola · `PGPASSWORD`
> yok · staging yok · staging salt-okunur · **staging dolu (8 MiB tmpfs)** →
> `No space left on device` ve hiçbir şey sevk edilmiyor). `pg-backup-ship.sh` on iki
> yol (config yok/boş/`RCLONE_CONFIG` yok · `BACKUP_REMOTE` yok/çıplak/sondaki eğik
> çizgi/remote değil/tanınmayan · staging boş · manifest'siz dump · saklama süresi
> sayı değil / 7'nin altında). **Hepsi adıyla mesaj verdi.**
> 🔴 **VE BU BATARYA KENDİ KODUMDA GERÇEK BİR HATA BULDU:** `: > "$probe" || fail …`
> salt-okunur bir mount'ta **kendi mesajını hiç basmıyordu** — `:` bir POSIX **özel
> builtin**'i ve yönlendirme hatası kabuğu **anında** sonlandırıyor, yani `||` hiç
> koşmuyor. `touch`'a çevrildi ve yeniden ölçüldü. Aynı sınıfın bir başka örneği
> baştan kaçınılarak yazıldı: sayımların **hiçbiri** `grep -c` kullanmıyor, çünkü
> `grep` sıfır eşleşmede exit 1 verir ve o, **sağlıklı boş veritabanı** yoludur.
>
> **ŞİFRELEME VE HEDEF — ÖLÇÜLDÜ, GERÇEK BİR `crypt` REMOTE'A KARŞI.** rclone'un
> `crypt` remote'u yerel bir backing store üzerinde koşturuldu: hedef diskteki dosya
> **adları** okunamaz (`uopkhhceg3gu…/e4cqqtph…`), **içerik** `RCLONE\0\0` sihirli
> baytıyla başlıyor, aynı hedef `rclone lsl` ile açık adlarla listeleniyor. Saklama
> süresi de izole edilerek ölçüldü: 2026-01-01 damgalı bir yedek **silindi**, o
> geceninki **kaldı**, boş dizin `rmdirs` ile toplandı.
> ⚠️ **`rclone check` bir crypt remote'ta `ERROR : No common hash found` basıyor ve
> bu bir arıza DEĞİL** (aynı çıktıda `0 differences found`, exit 0;
> `--ignore-checksum` bastırmıyor) — gecelik logda `ERROR` arayan operatörü
> yanıltmasın diye script'te ve README'de yazılı.
>
> **HEDEF SEÇİMİ: ÖLÇÜLDÜ, VE KÜMEDE HİÇBİR SEÇENEK YOK.** `kubectl get nodes` → tek
> node · `get sc` → yalnız `local-path` · `get csidrivers` → **boş** · `get pv` →
> 20/20 `local-path` · `get cronjob -A` → hiçbir namespace'te yedek işi **yok** (emsal
> de yok). Yani off-node hedef **kaçınılmaz olarak operatörün kimliğini** istiyor.
> **Elenenler, gerekçesiyle:** kümedeki bir PVC (aynı node'un diski — sınır 1'in
> tarif ettiği arızada hiçbir şey kurtarmaz) · GitHub Actions'ın dump'ı çekmesi
> (mesai kayıtları AB dışına ve DPA'sız bir işleyene — Q23) · ikinci bir Postgres
> replikası (tek node'da aynı disk; ayrıca yanlış bir `DELETE` replikaya **anında**
> kopyalanır — replika yedek değildir). **Seçilen:** AB bölgesinde S3 uyumlu nesne
> depolama ya da SFTP, ikisi de **tek bir `rclone.conf`** ile.
>
> **NEDEN CronJob OPERATÖRÜN, `deploy.yml`'in DEĞİL — ve bu FAZ D'nin dersinin
> uygulaması.** `01-rbac.yaml` deploy kimliğine `batch` grubunda yalnız `jobs`
> veriyor. `cronjobs` eklemek, **önce** cluster-admin ile 01-rbac.yaml'ın yeniden
> uygulanmasını gerektirirdi ve o ana kadar **her deploy kırmızı** olurdu — yani
> kartın imza kusurunun bir başka yüzü. Script'ler ise commit yolunda kalıyor:
> `deploy.yml` ConfigMap'i `--from-file` ile her koşuda yeniden kuruyor
> (`tappa-db-init` kalıbının aynısı), yani **tek tanım**, `scripts/` altında, ve
> `redline-check.sh`'in `SRC` listesinin **içinde** — `deploy/`'un dışarıda kalması
> (C4) bu sefer bir boşluk üretmedi.
> ⚠️ **Ve tam da o tarayıcı benim dosyamda bir bulgu üretti:** `pg-restore-verify.sh`
> ilk hâlinde çıplak `SET app.tenant_id` yazıyordu (R5 · WARN). Tek atışlık bir
> `psql` oturumunda havuz kirlenmesi **olamaz**, ama kural izlendi ve
> `BEGIN; SET LOCAL …; ROLLBACK;` yapıldı: bir sondanın uygulamayı modellemesi
> gerekiyorsa tenant bağlamını da uygulamanın yazdığı gibi yazmalı, ve tartışılması
> gereken bir tarayıcı görmezden gelinen bir tarayıcıdır.
>
> **SINIR LİSTESİ: 1 YENİDEN YAZILDI, 3 YENİ, 2 GÜNCELLENDİ — ve liste `1..21`
> ardışık, **0 sarkan anahtar atfı** (ölçüldü: tanımlı 15 anahtar, atıf yapılan 15,
> fark **boş**).** **[yedek YOK]** anahtarı **emekli edildi** çünkü artık yalan
> olurdu; yerine **[yedek kümede HENÜZ YOK]**, ve madde ağaç/küme ayrımını **adıyla**
> yazıyor (`kubectl -n tappa get cronjob` → **`No resources found`**, ölçüldü
> 2026-08-16). Yeni: **[yedeğin hedefi ve şifresi KÜME DIŞINDA yaşar…]** (19),
> **[yedek saklama süresi 30 gün…]** (20), **[geri yükleme AYRICALIK ARTIĞI üretir…]**
> (21). Güncellenen: **13** (yeni CronJob kuralın üçüncü tüketicisi ve `wait-for-postgres`
> taşıyor — kural **kapanmadı**, uyuldu) ve **14** (`rclone/rclone:1.71` hareketli
> etiketin **üçüncü** yazımı; digest pinleme artık dört yazım demek).
>
> **KARTIN HÂLÂ AÇIK KRİTERLERİ, açıkça:** *"managed Postgres"* (kullanıcı kararı) ·
> **KEK döndürme aracı** (sınır 9, **bu turda yapılmadı** — FAZ F) · **DPA/Q23 +
> saklama süresi** (hukuki, backlog B3). Ve *"geri yükleme denenmiş"* kriteri için
> dürüst cümle şu: **prosedür provalı, araç var, kümede yedek yok** — üçüncüsü
> operatör adımı 9'da.
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 2. TUR — GENEL ÜÇÜNCÜ GÖZ ONAY, `tappa-security-auditor`
> RED. BULDUĞUM KUSUR SANDIĞIMDAN AĞIRMIŞ: §4.3 DEĞİL, §4.4 + §4.7.**
>
> **B1 — 31 fazla yetkinin ikisi `tags` üzerinde, ve `tags`'te İKİNCİ KEMER YOK.**
> 1. turda *"§4.3'ün iki kemerinden biri düştü, tetikleyici hâlâ reddediyor, ürün
> bozulmuyor"* yazdım ve bunu **üç yerde** tekrarladım. Denetçi ölçtü:
> `tags_counter_monotonic` yalnız `BEFORE UPDATE … WHEN (new.last_ctr < old.last_ctr)`,
> yani **INSERT'te hiç ateşlemiyor**. Kendi yeniden üretimim, geri yüklenmiş gerçek
> şemada, `tappa_app` ile TCP üzerinden, tap kaydı olmayan bir plakette:
> ```
> A  ctr before                        5100
> DELETE FROM tags WHERE uid=...    -> DELETE 1     (yetki YALNIZ artıktan geliyor)
> INSERT ... last_ctr 0             -> INSERT 0 1
> C  ctr AFTER delete+reinsert         0
> D  a REPLAYED ctr=7 now advances to  7
> E  aes_key_ref is now                forged-key-ref
> ```
> **§4.4'ün replay koruması çalınmış bir `tappa_app` kimliğiyle tamamen sıfırlanıyor**,
> ve tablo düzeyi UPDATE sütun kısıtını ezdiği için `aes_key_ref` **yazılabilir**
> (`has_column_privilege`: kaynakta false, geri yüklemede **true**). `01-roles.sql`
> bu tehdit modelini adıyla yazıp *"§4.3 ayakta kalır"* diyordu — `tags` için
> kalmıyordu.
>
> **KALICI ÇARE ÖLÇÜLDÜ VE UYGULANDI — VE ÖLÇÜM DENETÇİNİN İDDİASINI HEM DOĞRULADI
> HEM DARALTTI.** Denetçi *"iki satırlık askıya alma 76 → 45 yapıyor"* dedi; o ölçüm
> **askıya alma adımına** aitti, önerdiği **daraltmaya** değil. İkisini ayrı ölçtüm:
>
> | kurulum | tablo yetkisi | `tags` UPDATE/DELETE | `aes_key_ref` UPDATE |
> |---|---|---|---|
> | kaynak (canlı şema) | **45** | false / false | false |
> | geniş varsayılan + geri yükleme (bugünkü ağaç) | **76** | **true / true** | **true** |
> | **dar varsayılan** + geri yükleme | **50** | false / false | false |
> | dar varsayılan + askıya alma + geri yükleme | **45** | false / false | false |
>
> Yani **daraltma ağır yarıyı yapısal olarak kapatıyor** (replay zinciri artık ilk
> adımda `permission denied for table tags` ile duruyor) ama **sıfıra indirmiyor**:
> 5 fazla kalıyor, aralarında `legal_documents` üzerinde tablo düzeyi SELECT — o da
> kendi sütun listesini ezer. **İkisi birlikte 45/0/0.**
>
> **DARALTMA TAZE KURULUMU BOZMUYOR — GERÇEK goose İLE ÖLÇÜLDÜ, İDDİA EDİLMEDİ.**
> `Dockerfile --target migrate` ile gerçek migration imajı derlendi ve iki taze
> kurulum (geniş / dar varsayılan) üzerinde 20 migration koştu; **ikisi de başarılı**,
> ve tablo yetkileri arasındaki **TEK fark** `goose_db_version` üzerindeki
> UPDATE+DELETE (goose'un kendi defteri, uygulama ona dokunmaz). Uygulama
> tablolarının hepsi **birebir**: 45 = 45, ve geniş kurulum canlı seed'lenmiş
> veritabanıyla da birebir (harness'ın kendisi böyle doğrulandı). `go test -race
> -count=1 ./...` dar veritabanına karşı (kullanıcının `tappa-db`'sine
> **dokunmadan**, ayrı bir konteynere `DATABASE_URL`/`DATABASE_MIGRATE_URL` ile):
> **exit 0, 22 paket ok, 0 FAIL, 0 atlanan DB testi**, ve testlerin gerçekten oraya
> yazdığı 700 tenant satırıyla kanıtlandı. Sebep de zaten yapıda: her migration
> istemediğini **açıkça REVOKE ediyor** (`REVOKE DELETE ON tags`,
> `REVOKE UPDATE, DELETE ON transactions`, … 23 ifade), yani hiçbiri varsayılana
> dayanmıyordu. ⚠️ `01-roles.sql` bir **init script'idir**, migration değil: yalnız
> boş `PGDATA` üzerinde koşar, yani **canlı veritabanını değiştirmez** — kazancı taze
> kurulumlar ve taze pod'a geri yüklemelerdir. Üç metin (araç başlığı, sınır 21, bu
> defter) *"yalnız §4.3"*ten **§4.4 + §4.7**'ye çekildi.
>
> **B2 — TEK KESİN *"EVET"* SENARYOSUNUN NUMARALI PROSEDÜRÜ YOKTU.** Karar tablosunun
> tek kesin EVET'i *"node'un diski gitti"*, ama sekiz adımın tamamı hasarlı
> instance'ın var olmasını varsayıyordu; taze pod yolu adım 4'ün içinde bir ⚠️ notuydu.
> Artık **A YOLU / B YOLU** ayrımı iki komutla başlıyor ve **B YOLU ayrıca numaralı**
> (B1–B6): **askıya alma B2, yani birinci sınıf bir adım**, doğrulama aracı B4'te
> **zorunlu** (*"çıkmazsa exit 0, uygulamayı açma"* — o yolda karşılaştırılacak bozuk
> bir veritabanı **yok**, yani tek kanıt odur), ve B5 kayıp aralığının **ilan
> edilmesini** istiyor çünkü orada ölçülecek kaynak yok. G1 ve G2 de bununla kapandı.
>
> **YEDİ UCUZ — hepsi ölçümle.**
> **S1** Doğrulayıcı **91 `ON TABLE` GRANT satırının 73'ünü** atlıyordu (sütun düzeyi),
> ve atlanan kümede `aes_key_ref`'i UPDATE dışında tutan **tek mekanizma** vardı.
> Artık iki düzey de karşılaştırılıyor: dump'ın sütun grant'ları **artı** tablo
> grant'larının sütunlara yayılmışı, `information_schema.column_privileges` ile
> birebir. 🔴 **Ve ilk hâli 39 YANLIŞ POZİTİF üretti** (*"missing"*), çünkü SQL'de
> **sütun düzeyi DELETE yoktur**; yayılım artık yalnız SELECT/INSERT/UPDATE/REFERENCES
> için yapılıyor (ölçüldü: `column_privileges` yalnız bu dördünü raporluyor).
> Sonuç: doğru geri yüklemede **45 tablo + 454 sütun**, exit 0; artıklı olanda
> **5 tablo + 11 sütun** fazlası adıyla, exit 1.
> **S2** Kayıp aralığı komutlarında `-v ON_ERROR_STOP=1` **yoktu**; ölçüldü: bozuk bir
> `COPY` bayraksız **exit 0**, bayrakla **exit 3** — yani boş bir CSV *"kayıp yok"*
> diye okunup adım 8 atlanabilirdi. İki `psql`'e de eklendi.
> **S3** 🔴 CSV `SELECT *` idi ve `gps_lat`/`gps_lng numeric(9,6)` **tam koordinat**
> dışa aktarıyordu (§4.2/§4.7). Artık 13 sütun **adıyla**; dışarıda bırakılanlar ve
> neden yazılı (GPS · `source_ip` · `tag_uid`/`ctr`/`sun_valid` · `trust`/`*_match`/
> `policy_*`). Yeniden giriş `channel='manual'` olduğu için hiçbirine ihtiyaç yok.
> Ölçüldü: 745 satırlık gerçek çıktıda **koordinat şeklinde tek alan yok** (17 *"gps"*
> eşleşmesinin hepsi §5'in `note` metni).
> **S4** İki Secret'a bölmenin gerekçesi **iddia ettiği özelliği sağlamıyordu** —
> ikisi de aynı namespace/etcd/node'da. Gerekçe *"ayrı okuyucu kümesi"*ne indirildi,
> ve emanet artık **kanıtlanıyor**: adım 9(b) küme içi ve emanetteki `rclone.conf`'un
> sha256 **öneklerini** karşılaştırıyor (değer basılmadan).
> **S5** `redline-check.sh` R3 şema nitelemesini **tanımıyordu** (`UPDATE
> public.transactions` → eşleşmiyor), ve nitelemeli ilk mutasyonu bu turun kendi aracı
> getirdi — yani kural **yanlış sebeple** yeşildi. Desen düzeltildi (istege bağlı şema
> öneki + tırnaklı tanımlayıcı); **mutasyon testiyle** doğrulandı: ekilmiş bir
> `UPDATE public.transactions` → **exit 1**, kaldırılınca **exit 0**; kontroller
> (`transaction_reviews`, `transactions_archive`, `SELECT`, `INSERT`) eşleşmiyor.
> Doğrulama aracına **dosya adıyla** dar bir muafiyet verildi ve bedeli sayıldı.
> ⚠️ **Kapatılmayan, sayılan:** R7'nin sır-log deseni **Go biçimli**, yani bu üç kabuk
> script'inde bir `echo "$PGPASSWORD"` **hiçbir kuralca yakalanmaz** (bugün temiz).
> **S6** `BACKUP_REMOTE` (kova + yol) iş loguna basılıyordu; `pods/log` yetkisi olan
> herkes okur. Artık yalnız **remote adı**, yol *redacted*.
> **S7** *"nothing left behind"* **bilinenden fazlasını iddia ediyordu**:
> `failedJobsHistoryLimit: 3` ve TTL yok, yani düşen Job'ın pod'u **tutuluyor**; pod
> nesnesi dururken kubelet'in `emptyDir`'i söküp sökmediği **ölçülmedi** ve öyle
> yazıldı, temizlik komutuyla birlikte.
> **G3** `kept="$(rclone lsf … 2>/dev/null | awk …)"` — listeleme düşerse **başarılı
> bir yüklemeden sonra** `done: 0 backup(s) retained` + exit 0 basıyordu; en
> ürkütücü cümle, en zararsız sebepten. Artık exit kodu ayrılıyor ve *"UNKNOWN (not
> zero)"* deniyor.
> **G4** awk sayacının `^COPY public\.` çapası **sayılmış limit** olarak yazıldı
> (bugün ulaşılamaz: hiçbir tablonun ilk sütunu serbest metin değil; olsaydı arıza
> yine **gürültülü** olurdu, çünkü 4b o tabloyu *"veri bölümü yok"* diye bildirir).
> **G5** Boş config korumasının **mekanizması yanlıştı** (*"rclone yerel yol sayıp pod
> içine yazar"*); ölçüldü (v1.71.2): tanımsız remote → `CRITICAL … didn't find section
> in config file`, exit ≠ 0, **dizin yaratılmıyor**. Koruma doğru, gerekçe düzeltildi.
> **G6** `grep -q 'a\|b'` BRE alternasyonu iki düz `grep -qF`'ye çevrildi.
>
> **DEĞİŞMEYENLER:** kümeye hâlâ **hiçbir mutasyon** uygulanmadı
> (`kubectl -n tappa get cronjob` → `No resources found`), kullanıcının `tappa-db`'si
> **yalnız okundu** (231 832 satır, başta ve sonda aynı), `ci.yml` **değişmedi**,
> `git status --short` → **0 adet `.go`**, canlı ürün `/healthz` `/readyz` **200**.
> **Yeni dosya sayısı değişmedi**; değişen: `scripts/db-init/01-roles.sql` (tek satır
> + gerekçe) ve `scripts/redline-check.sh` (R3 deseni).
>
> ---
>
> **✅ [FAZ E · 2026-08-16] 4. TUR — `tappa-security-auditor` ONAY. ÜÇ UCUZ, VE İKİSİ
> "SAĞLANMAYAN BİR GARANTİYİ İLAN ETMEK" SINIFININ YENİ ÖRNEKLERİYDİ.**
>
> **S1 — DARALTMA, İHLALİN EN PAHALI OLACAĞI İKİ VERİTABANINDA YÜRÜRLÜKTE DEĞİL.**
> `01-roles.sql`'e yazdığım *"unutulursa gürültülü `permission denied` alınır
> (fail-closed)"* cümlesi **koşulsuzdu** ve iki yerde yanlıştı. (a) Dosya bir **init
> script'i**: çalışan veritabanlarının `pg_default_acl`'i hâlâ `tappa_app=arwd`.
> (b) 🔴 **Ve dump'ın kendi son satırı varsayılanı yeniden GENİŞLETİYOR** —
> `GRANT SELECT,INSERT,DELETE,UPDATE ON TABLES TO tappa_app`, yani **her geri
> yüklemenin sonunda** hedef yine geniş. Dört adımda ölçüldü: dar init → **`ar`** ·
> geri yükleme sonrası → **`arwd`** · o anda yaratılan yeni tablo `tappa_app`'e
> **`arwd`** veriyor · var olan tablolar **etkilenmiyor** (45 yetki, `tags`
> UPDATE/DELETE false). **Somut sonuç: doğrulama ortamı üretimden KATI, ve ayrım
> kusuru GİZLER yönde** — `REVOKE` satırını unutan bir migration yerelde hiçbir fark
> üretmez, üretimde sessizce `arwd` verir; `pg-restore-verify.sh` de göremez, çünkü
> referansı dump'ın kendisi ve dump geniş varsayılanı **ilan ediyor**.
> ✅ **Orkestratörün *"değerlendir"* dediği çare ÖLÇÜLDÜ VE UYGULANDI:** geri
> yüklemenin **son adımı** tek ifadeyle geri daraltıyor
> (`REVOKE UPDATE, DELETE ON TABLES` — `SELECT, INSERT` bilerek kalıyor). Ölçüm:
> varsayılan `ar`'a dönüyor, yeni bir tablo `ar` alıyor, ve **var olan tablolar
> değişmiyor** (45 yetki · `tags` false/false · `aes_key_ref` false · sekans
> varsayılanı `rU` sabit). A YOLU 7. adıma ve B YOLU B3'e girdi, heredoc biçimi
> **birebir koşularak** sınandı (exit 0). ❌ **Çalışan veritabanında dayatılmadı** —
> canlı bir DB'yi migration dışında değiştirmek bir karardır; sınır **22** olarak
> sayılıyor, komutuyla birlikte. Üç metin (`01-roles.sql`, README'nin iki *"doğru
> davranır"* cümlesi) kapsamı artık **açıkça** yazıyor.
>
> **S2 — MASKE YALNIZ BAŞARI YOLUNDAYDI.** `BACKUP_REMOTE`'un kova+yolu iki **arıza**
> dalında maskesiz basılıyordu — üstelik biri script'in kendi yorumunun *"en olası uzun
> vadeli arıza"* dediği dal. Tek bir `REMOTE_SAFE` yazımı tanımlandı ve **her** mesajda
> kullanılıyor. ⚠️ **Ve kalan kısmı sayılıyor, yarım kapatılmıyor:** `rclone`'un
> **kendi** tanılama satırları remote'u adıyla basıyor (ölçüldü:
> `NOTICE: Encrypted drive 'enc:db/<damga>' …`). Maskeleyen bir filtreye **borulanmadı**,
> çünkü `set -eu` borunun solundaki hatayı göremez ve bir §4.7 sıyrığını fail-open bir
> çıkış koduyla takas etmek bu reponun iki kez bedelini ödediği takastır.
>
> **S3 — HEDEFİN GERÇEKTEN `crypt` OLDUĞUNU DOĞRULAYAN HİÇBİR ŞEY YOKTU** (kelime
> yalnız bir yorumda geçiyordu). Düz bir S3 kovası verilseydi her tap'in **tam
> GPS'i**, her plaketin **sarmalanmış AES anahtarı** ve her **oturum token hash'i**
> şifresiz node dışına çıkar, iş **yeşil** kalırdı. Artık remote'un **tipi
> sorgulanıyor**, `alias` zinciri izleniyor (5 sıçrama sınırı), ve `crypt` değilse
> **hiçbir şey gönderilmeden** duruluyor. **Dokuz dejenere yol koşuldu:** `crypt` ✓ ·
> `local` ✗ · `s3` ✗ · `alias→crypt` ✓ · `alias→local` ✗ · tanımsız ✗ · hedefsiz alias
> ✗ · alias döngüsü ✗ · `crypt`→`crypt` ✓; artı iki kontrol: önek karışması
> (`enc` vs `enc2`) **yok**, ve dört koşuda parola/markör çıktıda **0 kez**.
> 🔴 **§4.7 ölçümü aracın seçimini belirledi:** markörlü bir parolayla
> `rclone config show` **`password = *** ENCRYPTED ***`** basıyor (obscure değer 0,
> düz metin markör 0) ama `config dump` obscure değeri **2 kez** döküyor — bu yüzden
> `config show`, ve section'dan **yalnız `type` satırı** okunuyor. Sınır **19** artık
> dört ölçülemeyen özelliğin yanına **ölçülen beşinciyi** yazıyor.
>
> **🔴 VE BU TURUN İKİ KENDİ HATASI, DEFTERE.**
> **(1) §4.7 ölçümümün İLKİ GEÇERSİZDİ.** *"config show parolayı sızdırıyor mu"* diye
> baktım ve `grep -c "$OBS"` **19** dedi — oysa test config'imde `password` satırı
> **hiç yoktu**, yani `$OBS` boştu ve `grep -c ""` her satırı saydı. Markörlü bir
> parolayla yeniden kurdum. **Bu, bu reponun `01-roles.sql`'inde yazılı olan dersin
> aynısı** (*"bir doğrulama, tükettiği şeyin GÖRDÜĞÜ biçimi görmek zorundadır"*) — ve
> sayının büyüklüğü onu ele verdi, cevabın kendisi değil.
> **(2) Dejenere bataryam gerçek bir kusur buldu: `rclone check` YANLIŞ KAPSAMLIYDI.**
> İki damga taşıyan bir staging dizininde, doğru yüklenmiş bir yedekten sonra
> *"2 differences found"* deyip iş **kırmızı** oluyordu — çünkü karşılaştırma **tüm
> dizini** tek bir damganın hedefiyle eşliyordu. CronJob'da staging taze bir
> `emptyDir` olduğu için orada **oluşamaz**; elle koşan biri için oluşur. `--include
> "tappa-$stamp.*"` ile kapatıldı ve **iki biçimde de** ölçüldü: iki damgalı dizin
> **yeşil**, tek damgalı dizin **yeşil**. ⚠️ Kusuru bulan şey, testimin kendi
> dağınıklığıydı — ve bu tam olarak *"dejenere girdileri kendin say ve koştur"*un ne
> için olduğudur.
>
> **DEĞİŞMEYENLER:** kümeye **hiçbir mutasyon**, kullanıcının `tappa-db`'si yalnız
> okundu, `ci.yml` **değişmedi**, **0 adet `.go`**, canlı ürün **200/200**. Sınır
> listesi **1..22 ardışık**, **0 sarkan anahtar** (16 tanımlı / 16 atıf).
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 5. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BLOKLAYAN: `crypt`
> KONTROLÜM ŞİFRELEMEYİ DEĞİL, `type` SATIRINI ÖLÇÜYORDU — VE BU, OTURUMUN İMZA
> SINIFININ BENİM ELİMDEN ÇIKMIŞ HÂLİ.**
> Denetçi sevk edilen imajla ölçtü: **gerçekten `type = crypt`** olan bir remote'a tek
> bir seçenek eklemek yetiyordu — `no_data_encryption = true` ile script *"destination
> is an encrypted (crypt) remote"* deyip yüklüyor, doğruluyor ve **exit 0** veriyordu,
> oysa hedefteki dump **düz gzip** (`037 213 \b`) ve manifest **`cat` ile okunabilir**.
> 🔴 **Kendi dokuz dejenere yolumun dokuzu da yalnız `type` DEĞERİNİ değiştiriyordu;
> crypt'in KENDİ seçeneklerini bir kez bile değiştirmemiştim** — yani dedektör, bir
> önceki kusurun şekline göre yazılmıştı. Defterin kendi cümlesi, üçüncü kez.
>
> **SEÇİLEN ÇARE (2): BEYAN DEĞİL ÖZELLİK ÖLÇÜLÜYOR.** Yüklemeden **önce** crypt
> remote'una bir kanarya yazılıyor, **altındaki** remote'tan geri okunuyor ve iki şey
> assert ediliyor: saklanan baytlar rclone'un `RCLONE\0\0` sihirli baytıyla başlamalı,
> ve saklanan **ad** kanaryanın düz metin markörünü içermemeli. Neden yüklemeden önce:
> sonra koşan bir kontrol, hedefin şifresiz olduğunu ancak **çalışan kayıtlarını oraya
> yazdıktan sonra** kanıtlardı.
>
> **DEJENERE YOLLAR, BU KEZ CRYPT'İN KENDİ SEÇENEKLERİ ÜZERİNDE — on beşi de
> fail-closed, çıkış kodlarıyla:**
>
> | yol | rc | yol | rc |
> |---|---|---|---|
> | varsayılan crypt | **0** | `no_data_encryption = true` | **1** |
> | `filename_encryption = standard` | **0** | `filename_encryption = off` | **1** |
> | `filename_encryption = obfuscate` | **0** | ikisi birden | **1** |
> | alias → iyi crypt | **0** | `no_data_encryption = TRUE` (büyük harf) | **1** |
> | | | `no_data_encryption =␣true␣` (boşluklu) | **1** |
> | | | crypt, `remote =` yok | **1** |
> | | | alias → `nodata` crypt | **1** |
> | | | düz `local` · tanımsız remote | **1** |
> | | | bağlantı dizgisi · on-the-fly `:local:` | **1** |
>
> 🔴 **Büyük harf ve boşluklu varyantlar bu yaklaşımın neden seçildiğinin kanıtı:**
> özellik, seçeneğin nasıl **yazıldığını** umursamıyor. Bir `config show` ayrıştırıcısı
> (seçenek 1) o ikisinde kolayca yanılırdı.
> **Ve "kanıtlanamadı" ile "şifresiz" AYRI iki mesaj:** kanarya yazılamazsa /
> `cryptdecode` boş dönerse / alttaki remote okunamazsa iş *"COULD NOT BE PROVEN …
> this is NOT a claim that the destination is unencrypted"* diyor ve **yine hiçbir şey
> yüklemiyor**. ⚠️ Bu üçünden ikisi (kanarya yazımı, `cryptdecode`) fiilen tetiklendi;
> *"yazıldı ama alttan okunamadı"* dalı yerel bir backing store ile **üretilemedi** →
> **doğrulanamadı**, ve öyle yazıldı.
>
> **B1 — YENİ KONTROL, EN OLASI OPERATÖR HATASINDA TAMAMEN SESSİZ ÖLÜYORDU.**
> `rclone config show "$1" 2>/dev/null` + `set -eu` altında `||`'siz atama: elle
> yapıştırılmış bir Secret'ın **tek bozuk satırı** rclone'u `CRITICAL: Failed to load
> config file … could not parse line` ile düşürüyor, `2>/dev/null` sebebi yutuyor ve
> script **sıfır satır çıktıyla** ölüyordu (`rclone` ikilisi yoksa exit 127, aynı
> sessizlik). ⚠️ **Benim test ettiğim yol — tanımsız remote — `config show`'un exit 0
> verdiği yoldu**, yani sessiz olmayan tek yol. Artık stderr saklanıyor, çıkış kodu
> kontrol ediliyor ve rclone'un kendi cümlesi mesaja giriyor; bozuk config ile
> **ölçüldü**.
>
> **B2 — DOĞRULAYICI, BAĞLANTI HATASINI *"FORCE RLS yürürlükte değil"* DİYE TEŞHİS
> EDİYORDU.** Guard **iki dizeyi** sayıyordu; `CONNECT` alınmış bir DB'de gerçek hata
> `FATAL: permission denied for database "tappa"` ve o listede yok. Yapısal biçime
> çevrildi: sonda ilk ifadesinde bir **markör satırı** basıyor; markör yoksa oturum
> **hiç koşmamıştır**, sebebi ne olursa olsun. Ölçüldü — `CONNECT` alınmış DB:
> *"the tappa_app probe produced no answer … neither a pass nor an RLS finding. Fix the
> connection first. psql said: … permission denied for database"*; `CONNECT` geri
> verilince aynı komut **ok / ok / 42501**.
>
> **B3 — BÜTÜNLÜK KOMUTU OPERATÖRÜN KENDİ MAKİNESİNDE ÇALIŞMIYORDU.**
> `sha256sum -c <<<"…"` bu makinede (`sha256sum (Darwin) 1.0`) **`usage:` + exit 1**,
> Debian+GNU'da `OK` — yani **sağlıklı bir yedek**, kesinti anında *"yedek bozuk"* gibi
> okunan bir hata üretiyordu. Bu, README'nin 118–121. satırlarında **bedeli zaten
> ödenmiş** kalıp (*yerel araç ≠ hedef araç*), ve provam Linux konteynerlerinde
> koştuğu için o satır operatörün platformunda **hiç koşulmamıştı**. İki değişkeni
> karşılaştıran biçime çevrildi ve **iki lezzette de** ölçüldü. ⚠️ Ayrıca `$STAMP`
> artık elle veriliyor: `./restore/*.manifest` glob'u **iki damga** varsa iki hash
> basıp sessizce yanlış eşleştiriyordu (ölçüldü).
>
> **B4 — SAYILMIŞ SINIR DAR SAYILMIŞTI.** *"rclone'un kendi çıktısı yolu basıyor —
> **arızalı koşuda**"* yazmıştım; ölçüm: **exit 0 olan sağlıklı koşuda da** iki satır
> (`0 differences found` / `2 matching files`). Yani **her gece**. Yön doğruydu,
> kapsam yanlış; düzeltildi.
>
> **B5 — kapsam dışı, sayıldı:** `pg-backup.sh`'te `||`'siz komut-ikamesi atamaları
> **bilerek** duruyor ve gerekçesi dosyada: hepsi rol kapısının **kanıtlanmış**
> bağlantısından sonra koşuyor, ilk temasın kırılabildiği dört yol ise adlandırılmış
> mesaj taşıyor. Aynı şekil ship script'inde **kabul edilmedi**, çünkü orada config
> dosyasının kendisini koruyordu.
>
> **🔴 BU TURUN KENDİ HATALARI, DEFTERE.** (1) Özet harness'ım `cut -c` ile emoji'li
> satırları kırpıp **boş** gösterdi ve üç FAIL'i bir an *"mesajsız"* sandım — çıktının
> kendisi doğruydu, **ölçüm aracım** yanlıştı. (2) İlk doğrulama tablomda `set -- $c`
> kullandım; **zsh tırnaksız parametreyi kelimelere bölmez**, yani *beklenen* sütunu
> boş kaldı ve dört satır sahte *"MISMATCH"* gösterdi. İkisi de yalnızca raporlama
> katmanındaydı ve ikisi de temiz komutlarla yeniden koşuldu — ama ikisi de aynı
> dersin küçük hâli: **ölçüm aracının kendisi bir iddiadır.**
>
> **DEĞİŞMEYENLER:** kümeye hiçbir mutasyon, `tappa-db` yalnız okundu, `ci.yml`
> değişmedi, **0 adet `.go`**, canlı **200/200**.
>
> ---
>
> **🔴 [FAZ E · 2026-08-16] 7. TUR — YENİ ÜÇÜNCÜ GÖZ RED. BLOKLAYAN: KANARYANIN
> GERİ-OKUMA YOLU YANLIŞ ADRESE BAKIYORDU — VE BU, BİR TUR ÖNCE *"ÜRETİLEMEDİ"*
> DEDİĞİM DALIN TA KENDİSİYDİ.**
> `rclone cat "$crypt_under/$canary_stored"`. Crypt'in `remote =` değeri iki nokta
> üst üsteden sonra **yol taşımıyorsa** (`remote = back:`) bu ifade **`back:/<ad>`**
> üretiyor — **mutlak** bir yol; oysa kanarya remote'un **köküne** yazılmıştı.
> Sevk edilen imajla, kusursuz şifreleyen bir depoda ölçüldü:
> ```
> rclone cat back:<ad>    ->  R C L O N E \0 \0    rc=0     (gerçek)
> rclone cat back:/<ad>   ->  ERROR: error listing: directory not found, rc=3
> ```
> **Somut sonucu:** hedef **doğru şifrelerken** iş **her gece kırmızı**, **hiç yedek
> çıkmıyor**, ve mesaj operatörü **kimlik/ağ** aramaya gönderiyor — sebep
> `rclone.conf`'un tek satırıyken. 🔴 **Ve `remote = <ad>:` marjinal değil:** rclone'un
> **kendi sihirbazının belgelediği** biçim, ve README'nin önerdiği iki hedeften biri
> olan **SFTP** için **doğal** yazım.
> 🔴 **İKİ KENDİ HATAM BURADA BİRLEŞİYOR.** (1) Geçen tur *"'yazıldı ama alttan
> okunamadı' dalı yerel bir backing store ile üretilemedi"* yazmıştım — **üretildi**,
> tek config satırıyla. *Bir şeyin üretilemez olduğu iddiası da bir ölçümdür, ve o
> ölçüm yanlıştı.* (2) Aynı bloğun yorumu *"no alias arithmetic, **one code path**"*
> diyordu; `"$crypt_under/…"` **bir yol aritmetiğidir** ve bir yazımda yanlıştı — yani
> yorum, kodun yapmadığı bir şeyi ilan ediyordu.
> **Düzeltme bir `rjoin` yardımcısı:** sondaki `/` kırpılır, taban `:` ile bitiyorsa
> ad **doğrudan** eklenir, aksi hâlde `/` ile. **`remote =` yazımları üzerinde yedi
> dejenere yol, hepsi rc=0:** `back:` · `back:sub` · `back:sub/` · `back:sub/deeper` ·
> `/mutlak` · `/mutlak/` · alias arkasında; ve `back:` + `no_data_encryption=true`
> hâlâ **rc=1**. Kalıcı bir backing store'la tekrarlandı: hedefte gerçek nesneler
> **`RCLONE\0\0`** ile başlıyor, reddedilen koşuda hedef **0 nesne** (yani
> *"yüklemeden önce"* kararı yine kanıtlandı), ve **hiçbir yerde kanarya kalmıyor**.
> ⚠️ İlk bataryamda üç vaka **0 nesne** yazdı çünkü `local` backend'inde `root =`
> diye bir seçenek **yok** — sondam kendi kurgusuyla yanılmıştı; alias'lı kurulumla
> yeniden koşuldu.
>
> **SEKİZ MADDE.**
> **B1** Doğrulayıcının *"yapısal"* guard'ı yapısal değildi: markör satırı
> `SELECT … FROM public.transactions` idi, yani **ölçtüğü şeye bağlıydı**. Ölçüldü —
> `REVOKE SELECT ON transactions` ile oturum **açılıyor**, psql **bağlanıyor**, ve yine
> *"Fix the connection first"* diyordu. Artık ilk ifade **`SELECT 'PROBE-RAN'`** (hiçbir
> tabloya, politikaya, yetkiye dokunmuyor) ve **üç ayrı** sonuç var: oturum hiç
> açılmadı · açıldı ama tabloyu okuyamadı · gerçek RLS hükmü. İkisi de ölçüldü.
> **B2** `cryptdecode --reverse … 2>/dev/null` + çıkış kodu kontrolsüz — B1'in kendi
> gerekçesi 70 satır sonra uygulanmamıştı. rclone'un cümlesi artık mesaja giriyor.
> **B3** 🔴 **İDDİA 16'YI YANLIŞ ÖLÇMÜŞÜM.** *"`base64 -w0` bu makinede çalışıyor"*
> demiştim; **stdin** biçimini ölçmüştüm, README ise **dosya argümanı** kullanıyor.
> Gerçek: `base64 -w0 <dosya>` → **`invalid argument`, rc=64, STDOUT BOŞ**, yani
> `| gh secret set KUBE_CONFIG` **BOŞ BİR SECRET** yazardı. Portatif biçime çevrildi ve
> iki lezzette **aynı bayt** olduğu md5 ile doğrulandı.
> **B4** Kanarya, işin sahip olduğu önekin **dışına** (crypt kökü) yazılıyor ve silme
> `|| true` idi; artık başarısız silme **uyarı basıyor** ve dosyanın nerede kaldığını
> söylüyor. ⚠️ Silme hatası **üretilemedi** → doğrulanamadı, ve öyle yazıldı.
> **B5** Sabit kanarya adı + elle yaratılan Job'lar yarışabiliyordu (`Forbid` yalnız
> denetleyicinin Job'larını kapsar). Ad artık **koşu başına benzersiz** — yarış
> belgelenmedi, **kaldırıldı**.
> **B6** `[ "$live" -gt 0 ]` boş değişkenle **hata** verir ve `&&` zinciri tabloyu
> *"blind"* listesine **almaz** — fail-open. Sayısal guard eklendi.
> **B7** `psql … | sort -u > have.grants` borusu: psql düşse durum `sort`'un (0) olur,
> dosya boş kalır ve `comm` **45 yetkinin hepsini "MISSING"** diye raporlardı. Her
> sorgu artık durumu kontrol edilen bir dosyaya iniyor.
> **B8** `obfuscate` geçiyor — **sayılmış seçim** olarak sınır 19'a yazıldı: rclone'un
> kendi belgesine göre şifreleme değil geri çevrilebilir karıştırma; kalan maruziyet
> **yalnız adlar**, içerik bayt kontrolüyle şifreli.

---

> **Kart düzeltmesi (2026-08-16, M8-02 FAZ F uygulaması sırasında — KEK DÖNDÜRME
> ARACI).** FAZ E'nin kapanışı bu işi *"sınır 9, bu turda yapılmadı — FAZ F"* diye
> bırakmıştı. Bu tur getirdi: **`cmd/rotatekek`** · okuma yolunda **iki KEK**
> (`TAPPA_TAG_KEK_PREVIOUS`, `internal/config` + `sun.UnwrapAny`) · `deploy/README.md`
> → *"KEK döndürme"* prosedürü · sınır 9 yeniden yazıldı.
>
> **🔴 KARTIN KRİTERİ YETERSİZDİ, VE EKSİĞİ ÖLÇÜLDÜ.** Kriter yalnız *"tüm parkın
> `aes_key_ref` değerlerini yeniden sarmalayan araç var"* diyor. Yalnız o araç
> yazılsaydı **ürün kırılırdı**: park hangi satırın hangi KEK'le sarıldığını
> **kaydetmiyor** (`tags`'te sürüm sütunu yok), yani yeniden sarmalama sürerken park
> **karışıktır** ve tek KEK tutan bir süreç öteki yarıdaki her tap'e **500** döner —
> §4.6 ürünün ALTINDA çöker: kayıt `flag`'lenmez, **hiç alınmaz**. Kriterin
> gerektirdiği ama söylemediği şey **okuma yolunun değişmesiydi**.
>
> **ÜÇ YOL ÖLÇÜLDÜ, BİRİ SEÇİLDİ — ve elenenler sayıyla elendi:**
>
> | Yol | Ölçüm | Karar |
> |---|---|---|
> | **(A) iki-KEK okuma yolu** | pencereyi **iki yönde** kapatıyor (koşu **ve** geri alma); maliyeti bir isteğe bağlı env + `UnwrapAny` | **SEÇİLDİ** |
> | **(B) tek atomik transaction** | orkestratörün okuması **doğrulandı ve genişledi**: pencereyi kapatmıyor, **taşıyor** (pod COMMIT'ten sonra da eski KEK'i tutar) — üstelik **yeni bir arıza üretiyor**: 72 380 satırı tek transaction'da tutan bir koşuda ürünün kendi replay guard'ı (`AdvanceTagCounter`, satırda `FOR UPDATE`) **10,32 sn blokladı** | elendi |
> | **(C) `tags`'e KEK sürüm sütunu** | en ağırı (migration + §6 beşlisi) **ve tek başına yetmiyor**: satır hangi KEK'le sarılı olduğunu söylese bile o KEK'in **süreçte mevcut** olması gerekir, yani (A)'yı **yine** gerektirir | elendi |
>
> **(B) tamamen atılmadı — faydalı yarısı alındı.** Araç **tek set-tabanlı `UPDATE`**
> yazıyor, yani kilit penceresi ifade süresi kadar: aynı parkta **3,6 sn** (72 380
> satır) ve rotasyon sürerken atılan **12 canlı tap'in 12'si başarılı, en kötü bekleme
> 0,76 sn** — sayaç ilerledi **ve** sarmal değişti, kayıp güncelleme yok.
>
> **ARAÇ BİR FİLTREDİR** (bağlantı açmaz, DSN tutmaz, sürücü linklemez;
> `test/fixtures/seedkeys`'in emsali). Gerekçe kozmetik değil: `aes_key_ref`'i yalnız
> `tappa_owner` yazabilir (00013 `UPDATE(aes_key_ref)`'i `tappa_app`'ten aldı), ve
> *"hangi rol yazıyor"* sorusu `psql` çağrısında **bir kez** cevaplanıyor. Yan fayda:
> `cmd/tappa/packaging_test.go`'nun *"owner DSN'i `internal/config` dışında geçmez"*
> kapısı **genişletilmedi** — yeni kodu almak için bir kapıyı gevşetmek, kapıyı
> kaybetmektir.
>
> **§4.6'YA DEĞEN ÜÇ KARAR, ölçümleriyle:**
> ⚠️ **AŞAĞIDAKİ İKİ SAYI FARKLI KURULUMLARA AİTTİR VE BU SÖYLENMEMİŞTİ:** *"72 380
> satırın 39 062'si açılmıyor"* **gerçek geliştirme parkını** anlatır (çoğu satır
> test artığı, hiçbir KEK'le açılmaz); *"72 380/72 380 açılıyor"* ise **provanın
> sentetik ön-durumunu** anlatır (tüm park önce bilinen bir KEK altına konur).
> İkisi çelişmiyor, farklı deneyler — kartın onları ayırmaması bir kusurdu.
>
> 1. **Açılmayan satır sessizce ATLANMIYOR.** Gerçek parkta ölçüldü: 72 380 satırın
>    **39 062'si** hiçbir KEK'le açılmıyor (1/2/12 baytlık test artığı, **31 320
>    tenant**). Araç **varsayılan olarak reddediyor**; devam için operatörün sayıyı
>    **birebir** beyan etmesi gerekiyor, ve o koşunun çıkış kodu **3** — başarıdan
>    ayrı. ⚠️ **Sayılmış limit:** sayı hata mesajında yazıyor, yani beyan bir **hız
>    kesicidir — KAYIT DEĞİLDİR.** (Bu cümle *"hız kesici ve kayıt"* diyordu; 8.
>    turda README'de ve `main.go`'da düzeltildi, burada eski hâliyle kaldı ve 10.
>    turda yakalandı. `audit_log` satırı yok, dosyada iz yok, DB'de iz yok.)
> 2. **Sıfır satır fail-open DEĞİL.** Ölçüldü: `tappa_app`, `app.tenant_id` set
>    edilmeden, 72 380 satırın **0'ını** görür — **hatasız**. Araç 0 satırı
>    reddediyor ve mesajı **iki sebebi de** adlandırıyor.
> 3. **RLS'in DARALTMASI aracın göremediği yerde yakalanıyor** — ⚠️ **BU MADDE 1.
>    TURDA YARIM ÖLÇÜLMÜŞTÜ; 2. TUR DÜZELTTİ, ayrıntısı aşağıdaki F2 bloğunda.**
>    Ölçülen kurulum *daraltılmış okuyucu + **süper** yazıcı*'ydı: araç
>    *"15/15 ✓, EXIT 0"* dedi, son koşul yakaladı (`ERROR … 15 rows were read but
>    tags holds 72380 … Nothing written`, `psql` exit 3, park değişmedi).
>    **Runbook o kurulumu üretmiyordu** — tek `$OWNER_DSN` ile okuyan ve yazan aynı
>    daraltılmış oturumdu, ve o yolda son koşul **15'i 15'le karşılaştırıp
>    GEÇİYORDU**. Koruma 2. turda gerçek oldu: SQL artık saymadan önce oturumun
>    RLS'i **gerçekten atladığını** doğruluyor.
>
> **KAPSAM: `retired` + `lost` DAHİL** (5 853 + 1 812). Gerekçe araçtan değil buradan
> okunmalı: `lost` bir plaket **fiziksel olarak birinin elinde** ve anahtarı sızmış
> KEK'in altından **en çok çıkması gereken** anahtar; ayrıca **her dışlama sızmış
> KEK'i kalıcı olarak GEREKLİ kılar** (bir satır hâlâ yalnız onunla açılır), yani eski
> KEK'i yok etmeye izin vermeyen bir rotasyon rotasyon değildir. Maliyeti sıfır: aynı
> ifade.
>
> **KAPSAM DIŞI, açıkça:** **plaketin kendi AES anahtarı dönmüyor.** O çipe yazılıdır
> (ADR 0003 **md. 5 — "Anahtar döndürme"**; md. 3 anahtar ÜRETİMİDİR, ve md. 5 yerinde `ChangeKey`'i **MVP dışı** bırakır); döndürmek parktaki her NTAG 424 DNA çipini **fiziksel olarak
> yeniden encode etmek** demektir — saha operasyonu, bu görevin dışında, ve araçta +
> runbook'ta + burada üç kez yazılı.
>
> **DOĞRULAMA — provası atılabilir bir kopyada koşuldu** (`CREATE DATABASE …
> TEMPLATE tappa` → koş → doğrula → `DROP`; `tappa` veritabanına **0 yazma**).
> Dönüşün doğruluğu **bağımsız bir uygulamayla** ölçüldü (repo dışı, `internal/sun`'ı
> import **edemeyen** bir modül — yani kanıt formatın uyuştuğunu söylüyor, kodun
> kendisiyle tutarlı olduğunu değil): **72 380/72 380** yeni KEK'le açılıyor **ve aynı
> plaket anahtarını** veriyor (sha256 karşılaştırması, değer basılmadan), eski KEK'le
> açılan **0**. Yarıda kesilen koşu **idempotent**: yeniden koşulunca 72 380 *"already"*,
> 0 yeniden sarmalama.
>
> **19 mutasyon, 19'u kırmızı** (bir yirmincisi **davranışsal olarak etkisizdi** —
> `for i, kek` + `_ = i` — ve yeşil dönmesi hiçbir şey söylemez; ders **"DERLENMEYEN BİR MUTASYON YAKALANDI DEĞİLDİR"** gereği
> ayrıldı ve sayılmadı). `internal/sun` kapsamı **%96,7 → %97,0**.
>
> **🔴 KAPANMAYAN, VE SINIR 9'A YAZILDI: prosedür KÜMEYE karşı hiç koşulmadı.**
> Yerel bir Postgres konteynerine karşı koşuldu; ölçülen her şey **mekanizma**
> hakkındadır, küme hakkında **tek iddia yoktur** (ders **"BİR RUNBOOK'UN DOĞRULUĞU AĞACA GÖRE DEĞİL, KÜMEYE GÖRE YARGILANIR"**). Kapatan tek ölçüm,
> operatörün `tappa-postgres:5432`'ye **nereden** ulaştığıdır —
> `12-networkpolicy.yaml` 5432'yi yalnız `tappa` namespace'inde
> `app.kubernetes.io/name: tappa` etiketli pod'lara açıyor ve `kubectl port-forward`'un
> bu kuralın etrafından dolaşıp dolaşmadığı bu repoda **hiç ölçülmedi** (bu ajan küme
> üzerinde mutasyon fiili koşmuyor). ⚠️ İkinci sayılmış açık: rotasyonun **üçüncü
> adımı rotasyonun kendisidir** (`TAPPA_TAG_KEK_PREVIOUS`'u kaldırmak) ve onu zorlayan
> **mekanik bir kapı yok** — tutan tek şey prosedürün metni.
>
> **🔴 [FAZ F · 14. TUR] 12. TURUN KENDİ DÜZELTMESİ RUNBOOK'U KIRDI — VE BU, ON ÜÇ
> TURDUR TEKRAR EDEN SINIFIN SAF HÂLİYDİ. BİÇİM MEKANİKLEŞTİRİLDİ.**
>
> **B1 — `--apply` RUNBOOK'TA HİÇ GEÇMİYORDU.** 12. tur script'in varsayılanını ters
> çevirdi (yazmak `--apply` ister); runbook güncellenmedi ve
> `./scripts/rotate-kek.sh   # uygular` satırı **çıplak** kaldı. 77.215 satırlık
> kopyada **birebir o satır** koşuldu: **rc=0**, *"DRY RUN — nothing applied"*, ve
> **77.215 satırın tamamı ESKİ KEK altında** — üstelik runbook'un tablosu o sıfırı
> *"uygulandı"* diye okuyordu. Operatör adım 3'e geçseydi sunucu yalnız yeni KEK'i
> tutar, park tamamen eski KEK altında kalırdı: **her tap 500, hiç mesai kaydı
> yok**, ve sızmış KEK tek çalışan anahtar. Kartın *"yazılı **ve yürütülebilir**"*
> kriteri doğrudan yanlışlanmıştı.
>
> 🔴 **SINIF: bir artefakttaki düzeltme başka bir artefakttaki cümleyi sessizce
> yanlışlıyor ve ikisini bağlayan mekanik bir şey yok** (kart↔README beş kez,
> manifest↔config bir, kod↔test-adı iki, şimdi README↔script). **Çare bu görevde bir
> katman aşağıda zaten bulunmuştu** — *"script'i koşturarak pinlendi, tarayarak
> değil"* — ve bu turda **bir katman yukarı** uygulandı:
>
> **RUNBOOK'UN KOMUTLARI ARTIK TARANMIYOR, KOŞULUYOR.**
> `TestRunbook_ItsOwnCommandsAreRUNNotSCANNED` rotasyon bölümünden
> `./scripts/rotate-kek.sh …` satırlarını **çıkarıyor** ve her birini **gerçek
> script'e** stub psql ile **koşturuyor**, sonra yıkıcı adıma ulaşıp ulaşmadığını
> ölçüyor: `--apply` taşıyan satır **ulaşmalı**, taşımayan **ulaşmamalı**, ve **en az
> bir** satır ulaşmalı (yoksa runbook hiçbir şey döndüremez). Ölçüm: **4 çağrı, 2'si
> uygulama adımına ulaşıyor**; README çıplak hâline döndürülünce test *"NONE of the 4
> … reaches the apply step"* ile **kırmızı**. Çıkış kodu tablosunun satırları da
> **gerçek koşularla** eşleştirildi (1 ve 3; **0 sayılmış limit** — her satırın
> açıldığı bir park ister, o prova atılabilir kopyada).
>
> **B2 — BYPASS SONDASI psql'İN KODUNU OLDUĞU GİBİ SIZDIRIYORDU.** `set -e` altında
> korumasız bir komut ikamesiydi: sonda 3 dönerse **script 3** — ve 3 yayımlanmış
> bir anlam (*"uygulandı ama park tam dönmedi, eski KEK yok edilemez"*), oysa park
> **hiç okunmamıştı**. Ölçüldü ve haritalandı: **3/2/4/127 → 1**, her biri psql'in
> kendi durumunu adlandırarak. ⚠️ **Ve script'in kendi yorumu *"bu, korumasız kalan
> son çağrıydı"* diyordu — YANLIŞTI**, düzelttiğini söylediğinin **59 satır
> yukarısındaydı**; yorum düzeltildi. Stub artık başarısızlık yolunu **gerçekten
> sürüyor** (eskiden `*rolsuper*`'a koşulsuz `echo t; exit 0` diyordu, yani koruma
> **yapı gereği test edilemezdi**).
>
> **NB3 — dedektör iki KARAKTERİ kapsıyordu, sınıfı değil.** Gerçek pty'de ölçüldü:
> **backtick ÇALIŞIYOR · `$( )` ÇALIŞIYOR** (ikisi de işaret dosyası yazdı) ·
> `!` `~` `*` `${}` **yan etki üretmedi**. `$( )` eklendi, ve testin **kapsamı
> dürüstçe yazıldı**: iki yapı ölçülüp reddediliyor, üçü ölçülüp zararsız bulundu,
> gerisi **sayılmış limit** (bu bir denylist).
>
> **NB4 — `danglingBudget` bir SU-SEVİYESİ İŞARETİYDİ.** `>` ile: bir atıfı onar +
> satırı sil → 26/27 yeşil; sonra **yepyeni bir sarkan atıf + yeni satır** → 27/27
> **yeşil**, yani hiçbir Go dosyası düzenlenmeden kalıcı bir muafiyet satın
> alınıyordu — ve dosyanın kendi yorumu ile **kartın 13. turdaki cümlesi** bunu
> *"mevcut boyut"* diye tarif ediyordu (bu görevde *"düzeltildi"* iddiasının
> **altıncı** yanlışı). Tek satır: `>` → **`!=`**.
>
> **NB5 — yoruma alınmış bir test hâlâ *"tanımlı"* sayılıyordu** (`^func Test…(`
> `/* */` içinde de eşleşiyor), yani *"silinen test, hayatta kalan atıf"* sınıfı
> **silmek yerine yoruma almakla** erişilebilirdi. Blok yorumlar artık soyuluyor.
>
> **AÇIK İKİ KALEM KAPANDI.** `README`'nin `$WORK`/`go build` bloğu **ölüydü** —
> `$WORK/rotatekek` hiçbir yerde çalıştırılmıyordu — **ve bir yan etkisi vardı:**
> o bloğun `trap … EXIT`'i adım 2'nin `PGPASSFILE` trap'i tarafından **eziliyordu**,
> yani geçici dizin **koşulsuz sızıyordu**. Blok kaldırıldı; `go build -o` iddiası
> README'den **script'e taşındı** (README'de tutmak ölü kodu canlı tutardı). Ve
> *"2 restart, 5 status"* sayımı düzeltildi: **çalıştırılabilir** satırlarda
> **2 `rollout restart`, 3 `rollout status`**.
>
> **🔴 [FAZ F · 10.–12. TUR] MERKEZÎ KARAR: PROSEDÜR ARTIK BİR SCRIPT —
> `scripts/rotate-kek.sh`. VE 12. TURDA O SCRIPT'İN KENDİSİ KRİTİK BİR KUSUR
> ÜRETTİ.**
>
> **10. TURUN KARARI.** Bulguların dördü tek sınıftandı: **bir runbook, belirtilmemiş
> bir dilde yazılmış bir programdır** (operatörün kabuğu). Gerçek pty'de `zsh -f -i`
> ile yapıştırılan bloklarda bir **`#` yorumundaki backtick ÇALIŞIYOR** — en keskin
> örnekte yorumun uyardığı ikilinin yolunu yorumun kendisi koşturuyordu — ve tek
> apostrof **sonraki komutu yutuyordu**. Prosedür FAZ E'nin üç script'inin şeklinde
> bir dosyaya taşındı; kabuk **belirtildi** (`bash`, shebang + `bash -n` süitte).
> Bununla `ON_ERROR_STOP` (yokluğunda **40 ref sunucu log'una + psql rc=0**),
> `mktemp -d`, `trap`, ve aracın çıkış kodunun maskelenmemesi **operatörün
> parmaklarından çıkıp sürecin içine** girdi.
>
> **🔴 12. TUR — VE EN AĞIR KUSURU BEN ÜRETTİM: `--help` BÜTÜN PARKI DÖNDÜRÜYORDU.**
> Argüman okuma tek bir tam-dize karşılaştırmasıydı, yani `--dry-run` DIŞINDAKİ
> **her** girdi yıkıcı dalı seçiyordu. Atılabilir 75 662 satırlık kopyada, park
> parmak izi önce/sonra: `--dryrun` · `--help` · `-n` · `--dry-run=true` ·
> `x --dry-run` — **hepsi rc 0, hepsi PARKI YENİDEN YAZDI.** Rotasyon eski KEK ve
> ikinci bir koşu olmadan geri alınamaz; sunucular iki KEK'i tutmuyorsa sonuç her
> tap'te 500 ve **hiç kayıt yazılmaması**. Ders **"sağlıklı sistem BOŞTUR, arızalı
> sistem EKSİKTİR"**in tam şekli, ve bu kez sonucu geri alınamaz.
> **İki okuma ölçüldü ve ikincisi seçildi:** (a) yıkıcı varsayılanı koruyup katı
> `case` yazmak — geriye yine tek yıkıcı girdi kalır (**boş** olan), yani gelecekteki
> her ayrıştırma hatasının düşeceği yer *"parkı yeniden yaz"*tır; (b) **tersine
> çevirmek** — yazmak `--apply` ister, ve bir yazım hatasının, bilinmeyen bayrağın ya
> da ayrıştırma hatasının düşeceği yer **güvenli** daldır. Geri alınamaz bir işlem
> için soru budur. **(b) uygulandı**, ve on argv şekli park parmak iziyle ölçüldü:
> **hepsi değişmedi**, `--apply` pozitif kontrolde uygulama adımına **ulaştı**.
>
> **psql'İN ÇIKIŞ KODU HARİTALANDI.** Uygulama adımı `|| die` taşımayan tek psql
> çağrısıydı: stub psql 3 ile çıkınca **script 3** veriyordu — ve **3 yayımlanmış bir
> anlam**: *"uygulandı ama park tam dönmedi, eski KEK yok edilemez"*. Oysa hiçbir şey
> uygulanmamıştı. Artık psql'in her sıfırdan farklı kodu **1**'e (hiçbir şey
> uygulanmadı) haritalanıyor ve psql'in kendi kodu mesajda **adlandırılıyor**;
> ölçüldü: 3→1, 2→1, 1→1, ve psql başarılıyken aracın **kendi** 3'ü korunuyor.
>
> **SINIF ÇATALLANMIŞTI, İKİNCİ UCU DA KAPATILDI.** Bölümde hâlâ **13 yapıştırılan
> `bash` bloğu** var (sır girişi, `kubectl` adımları, doğrulama). Tehlike **karakter
> düzeyinde** kaldırıldı: yorumlarda **0 backtick, 0 tek apostrof**, ve bloklar
> `bash -n` **ve** `zsh -n` altında temiz. 🔴 **Ama *"imkânsız"* iddiası
> **SAYIYA İNDİRİLDİ** — tablo artık 14 yapıştırmayı adıyla söylüyor ve bunun bir
> **özellik** olduğunu, yapısal bir imkânsızlık **olmadığını** yazıyor. Kontrol:
> README şeklindeki backtick'li bir yorum gerçek zsh'te **çalıştı** (işaret dosyası).
>
> **`$PGPASSFILE` ARTIK SİLİNİYOR VE TEK MEKANİZMA.** `~/.pgpass` yolu **elendi**:
> temizliği literal `HOST` arayan bir `sed`'di, gerçek bir dosyada **hiç
> eşleşmiyordu**, süper kullanıcı parolası diskte kalırken *"kalan owner satiri: 0"*
> basıyordu — kendi beklentisini doğrulayan koşulsuz bir no-op. Ve **`rm -P` yalanı
> runbook'tan da kalktı** (script'te zaten sondalanıyordu): bu platformda `rm -P`
> belgeli bir no-op ve **0 döner**, `shred` yok, yani `|| shred` dalına hiç
> geçilmiyordu.
>
> **MANDAL DÜZELTİLDİ.** Envanter **taranan bir `.go` dosyasındaydı**, yani her
> girdi **kendini alıntılıyordu**: bayatlama kontrolü hiç ateşleyemezdi ve bir atıfı
> onarmak **sıfır sinyal** üretiyordu. Envanter `cmd/tappa/testdata/`'ya taşındı
> (taranmıyor), bütçe **mevcut boyuta** mandallandı (bir satır eklemek insan
> düzenlemesi ister), *"tarihli"* yorum iddiası kaldırıldı, ve kapsam **`docs/adr/`
> + `CLAUDE.md` + `.yml`** ile genişletildi — üçü de daha önce **yeşil** geçiyordu,
> `docs/adr/` üstelik CLAUDE.md §10'un ADR zorunlu kıldığı yer.
>
> **⚠️ VE BİR PROTOKOL KUSURU ORKESTRATÖRÜN ÖLÇÜMÜYLE DÜZELDİ:** on bir turdur
> mühürlenen `git diff | shasum` yalnız **tracked** dosyalardı; bu işin çekirdeği
> (`cmd/rotatekek/`, `scripts/rotate-kek.sh`, üç test dosyası) **untracked** olduğu
> için bütünlük kontrolünün **dışındaydı**. Tam kapsamlı hash artık kullanılıyor.
> Bu, tam olarak bu görevde beş turdur kovalanan sınıf: **bir doğrulama aracının
> kendi kapsamı ölçülmemişti.**
>
> **🔴 [FAZ F · 8. TUR] İKİ YENİ DENETÇİ DE RED — VE BU TURUN DÖRT BULGUSUNUN
> DÖRDÜ DE KODUN DIŞINDAKİ BİR VARSAYIMDI: KABUK · DOSYA SİSTEMİ · SUNUCU
> YAPILANDIRMASI · BORU ANLAMBİLİMİ.**
>
> **B1 (KRİTİK, §4.7) — BAŞARISIZ HER ROTASYON PARKIN SARMALLARINI SUNUCU LOG'UNA
> DÖKÜYORDU.** Üretilen SQL tek bir ~19 MB'lık `DO` ifadesiydi ve `VALUES` listesi
> onun **içindeydi**; Postgres hata veren bir ifadenin **tam metnini** log'lar
> (`log_min_error_statement` varsayılanı `error`). Kendi ölçümüm, 20 satırlık
> sondada, **ön koşul 6 birebir sağlanmışken** (`log_statement='none'`,
> `log_min_duration_statement=-1`): **40 ayrı 88-hex sarmalı ref** log'da — 20
> eski, **20 YENİ**. Yeni KEK altındakiler rotasyondan **sonra da** hassas, ve
> runbook abortları **rutin** sayıyor.
> **Biçim kararı: (a) ön koşula üçüncü bir değer yazmak bir YAPILANDIRMA
> iddiasıdır** — operatör ya da managed sağlayıcı düşürünce düşer. **(b) seçildi:
> ifadeyi küçült.** Sarmallar artık `CREATE TEMP TABLE` + `COPY … FROM STDIN` ile
> **COPY VERİSİ** olarak gidiyor; COPY verisi ifade metni **değildir**. Üstelik
> guard'lar (tenant GUC · `rolsuper` · park büyüklüğü) artık **COPY'den ÖNCE**
> koşuyor, yani runbook'un *"rutin"* dediği abort tek bir ref bile göndermeden
> düşüyor. Temp tablo ayrıca **WAL'a yazılmaz** — aynı baytlar yedeklere ve
> replikalara da girmiyor.
> **Kontrollü ölçüm (aynı oturum ayarları, aynı DB, işaretlenmiş log penceresi):**
> **eski şekil 40 ref · yeni şekil 0 ref.**
>
> **B2 (KRİTİK, §4.7) — ZSH'TE EMANET SATIRI ESKİ KEK'İ EKRANA YANKILIYORDU.**
> `read -rs -p` zsh'te *"coprocess'ten oku"* demek: `read` anında 1 dönüyor, `&&`
> zinciri kırılıyor, dosya **0 bayt** kalıyor, ve girdi **tüketilmediği** için
> yapıştırılan KEK bir komut olarak çalışıp `command not found: <KEK>` ile ekrana
> basılıyor — **iki satır yukarıdaki uyarının tam olarak engellemek için yazıldığı
> sonuç**. Yerine POSIX biçim: ayrı `printf` + `stty -echo` + `IFS= read -r`.
> **İkisinde de ölçtüm: bash ve zsh — 21 bayt, mode 600, yankı 0.**
>
> **B3 (§4.7) — `/tmp/kek_new.b64` çıplak `>` ile yazılıyordu ve önceden konmuş
> symlink'i İZLİYORDU** (`umask` var olan dosyanın iznini değiştirmez). Teknik
> **zaten dosyadaydı**: kardeş satır `install -m 600 /dev/null` kullanıyor ve
> saldırıyı yeniyor; yalnız birine uygulanmıştı. İkisine de uygulandı.
>
> **B4 (§4.7+§4.5) — `go build -o /tmp/rotatekek` başarısızlığı hiç kontrol
> edilmiyordu**, ve o yolda symlink/dizin/eski-ikili varken `go build` **0
> dönüyor**. Sonraki adım iki KEK'i ortama koyup o yoldaki şeyi koşuyor ve
> **çıktısını süper kullanıcı psql'e** veriyordu. Artık `mktemp -d` (0700) +
> `trap … EXIT` + `|| exit 1` + `-x` kontrolü.
>
> **B5 — YAYIMLANAN ÇIKIŞ KODU TABLOSU KENDİ BORUSUNDAN OKUNAMIYORDU.** `refuse()`
> zehir ifadesi basıyor, `psql` 3 ile ölüyor, `pipefail` **en sağdakini** veriyor
> ve aracın **1**'i maskeleniyor — yani *"REDDETTİ, hiçbir şey yazılmadı"* kodu bu
> boruda **erişilemezdi**. 6. turda `go run` için düzelttiğim şeyin **bir katman
> yukarısı**. **Biçim kararı: boruyu bıraktım.** Araç koşuyor → **kendi kodu
> kapılanıyor** (`rc=$?` + `case`) → sonra dosya psql'e veriliyor. Bedeli yazıldı:
> SQL artık `mktemp -d` içinde `install -m 600` bir dosyaya spool ediliyor, ve bu
> ancak B1 sayesinde kabul edilebilir.
>
> **B6** onay değişkeninde `export` yoktu (düz atama araca ulaşmıyor: exit 1 vs 3).
> **B7 — `50-backup` iddiası DÖRDÜNCÜ turda da yanlıştı, ve kök nedeni buldum:**
> 6. turdaki betikte `open(...).write(s)` **bütün düzenlemelerden SONRA** duruyordu;
> ikinci `assert` patlayınca dosya **hiç yazılmadı** ve **başarılı olan ilk
> düzenleme de onunla birlikte çöpe gitti**. Yani eklediğim `assert` sessiz
> no-op'u yakalıyor ama **iptal edilen bir toplu düzenlemeyi** yakalamıyordu. Bu
> turda her düzenleme **kendi yazımını** yapıyor ve her iddia yazılmadan **önce**
> `grep` ile doğrulanıyor. ⚠️ Ve aynı sınıfın ikinci örneği bu turda çıktı: *"kalan
> çıplak ders numarası 0"* iddiam **satır-bazlı** bir grep'le doğrulanmıştı ve
> **satır sonuna sarılmış** bir `(ders\n> 395)` atfını göremiyordu — kontrol artık
> **sarma-duyarlı**.
>
> **DENETÇİLERİN ALTI YEŞİLİ:** initContainer/sidecar `envFrom` **kırmızı**
> (kapı **pod düzeyine** genişletildi — iddia zaten pod düzeyinde yazılıydı) ·
> `env:` kaynak **cinsi** **kırmızı** (anahtarlar `secretKeyRef` olmak zorunda) ·
> config tarama regex'inin daraltılması **kırmızı** (sayı değil, **ada göre**
> pinlendi: bir sayım kapsam değildir) · `exactly-one-configMapRef` **kırmızı** ·
> `go run` varyantı **kırmızı** · boru **kırmızı**. ⚠️ **Kendi yeni kapımda iki
> yeşil buldum ve söylüyorum:** biri **etkisiz bir mutasyondu** (hiçbir şeyi
> yeniden sıralamıyordu, yeşili anlamsız), diğeri **gerçek bir boşluktu** —
> yalnız AÇILIŞ dolar-tırnak etiketini yeniden adlandırmak **geçersiz SQL**
> üretiyor ve hiçbir test görmüyordu; artık etiket **dengesi** pinli.
>
> **UCUZLAR:** `readAck` ham ortam değerini **stderr'e basıyordu** (ölçüldü: yanlış
> değişkene yapıştırılan bir KEK raporda görünüyordu, ve runbook o raporu
> **saklatıyor**) → artık yalnız **uzunluk**. `main.go`'daki *"and a record"*
> cümlesi README'nin kendi düzelttiği yanlıştı → düzeltildi. `.pgpass` deseni
> daraltıldı ve kapanış sayımı `|| true` ile bitiyor. `rolsuper` cümlesi olgu
> kipinden **ölçüm+beklenti** kipine çevrildi.
> 🔴 **MANAGED POSTGRES: adlandırılmış sayılmış limit yazıldı** — `BYPASSRLS`'i
> yalnız süper kullanıcı verebildiği için prosedür o dünyada ön koşul 3'te **durur
> ve devamı yoktur**; bilinen iki çıkış yolu da (`NO FORCE ROW LEVEL SECURITY` ya
> da ürün rolüne yazma hakkı) **bir ADR ister**, o yüzden sessiz bırakılmadı.
>
> **🔴 [FAZ F · 6. TUR] ÜÇÜNCÜ GÖZ RED — VE DENETÇİNİN KENDİ SAYIMI ŞUYDU:
> DAVRANIŞ 5 · METİN 6. BLOKLAYANLARIN İKİSİ DAVRANIŞ.**
>
> **B1 — SINIF KAPATICI, KAPATTIĞINI İDDİA ETTİĞİ SINIFA KARŞI SAVUNMASIZDI.**
> `20-app.yaml`'ın **mevcut `envFrom:` listesine iki satır** (`- secretRef: {name:
> tappa-secrets}`) eklemek `tappa-secrets`'ın **tamamını** sunucu sürecine enjekte
> ediyordu — içinde `DATABASE_MIGRATE_URL`, yani `rolsuper=t rolbypassrls=t` bir
> DSN — ve `go test ./cmd/tappa/` **exit 0** kalıyordu. Kendi yeniden üretimim
> aynısını verdi. 🔴 **Ve `20-app.yaml`'ın kendi yorumu tam olarak bunun
> olamayacağını, üstelik bu testin engellediğini yazıyordu.**
> **Sınıfın dördüncü örneği, ve sebebi hep aynı: kapı bir YOKLUK okuyordu**
> (*"`DATABASE_MIGRATE_URL` adı listede yok"*) — oysa **`envFrom` hiç ad taşımayan
> bir enjeksiyondur**. Bir ad listesi, enjeksiyonun sonsuz biçiminden yalnız birini
> kapatır.
> **Biçim düzeltmesi:** kapı artık yorumun iddia ettiği **mekanizmayı** iddia
> ediyor — *"servis eden konteynerin ortamı yalnızca açık `env:` girdilerinden
> gelir, ve `envFrom` **yalnız `configMapRef`** taşır"* — pozitif bir cümle, bir
> komutun çözdüğü. Enjeksiyon biçimlerini **saydım** (env: literal + 5 `valueFrom`
> türü, envFrom: `configMapRef`/`secretRef`) ve kapıyı listeye değil **kategoriye**
> bağladım. `envFrom` ayrıştırıcısının kendi dejenere girdileri de var (yalnız
> ConfigMap · listeye eklenmiş Secret · tek başına Secret · akış stili · hiç
> `envFrom` yok).
>
> **B2 — YAYIMLANAN ÇIKIŞ KODU TABLOSU, RUNBOOK'UN KENDİ KOMUTUYLA TESLİM
> EDİLEMİYORDU.** Ölçtüm (Go 1.26.5 + beş satırlık kontrol programı): `go run`
> altında **3 → 1**'e çöküyor, derlenmiş ikili **3** veriyor. Tabloda **1 ve 3 zıt
> anlamda** (*"hiçbir şey yazılmadı"* ↔ *"SQL yazıldı, park YARIM döndü, eski KEK
> imha EDİLEMEZ"*), yani operatör tam tersini okurdu — `main.go`'nun kendi
> gerekçesinin (*"the whole failure this program exists to prevent is an operator
> believing a rotation finished"*) tersi. **Ve bu, 4. turda kendi düzelttiğim
> *"test edilmiş ama kimsenin çağırmadığı fonksiyon"* şeklinin bir katman
> yukarısıydı:** sabitler pinliydi, **teslim** pinsizdi.
> **Düzeltme:** runbook artık **`scripts/rotate-kek.sh`'i çağırıyor**; derleme ve
> ikiliyi koşturma script'in içinde, `$WORK/rotatekek` yolunda (10. tur), ve iki yeni pin var:
> `TestExitCodes_TheCOMPILEDBinaryReallyDeliversThem` üç kodu **gerçekten derlenmiş
> ikiliyi çalıştırarak** ölçüyor, `TestRunbook_InvokesTheToolSoEveryExitCodeIsReadable` ise
> README'nin `go run`'a ya da boruya geri dönmesini engelliyor. İkincisi yazıldığı anda
> **kırmızıydı** (runbook hâlâ `go run` diyordu), yani ateşlediği gösterildi.
>
> **B3 — KARTIN *"YERİNDE DÜZELTİLDİ"* İDDİASI ÜÇÜNCÜ KEZ YANLIŞTI.** 4. tur
> *"ADR atfı md. 3 → md. 5 yazıldı"* diyordu; **üç yerin üçü de hâlâ `madde 3`**'tü.
> Sebebini buldum ve utanç verici: o turdaki `replace()` çağrısı **`assert`
> taşımıyordu**, yani hedef eşleşmeyince **sessizce hiçbir şey yapmadı** — bu turda
> her düzenleme `assert old in s` + `count==1` ile koşuyor ve bu blok yazılmadan
> **önce** `grep` ile doğrulandı: kalan `madde 3` atfı ölçüldü (bu iddia 12. turda yeniden
> koşuldu), üç dosya da `md. 5`
> ve **`ChangeKey`'in MVP dışı** olduğunu adıyla yazıyor.
>
> **DENETÇİNİN ÜÇ YEŞİLİ — üçü de kırmızı:** **M-C** (`closed` satırı `Info` →
> `Debug`) — sebebi bendeydi: test yardımcım `slog.LevelDebug` kuruyor, yani **sevk
> edilen seviyeyi hiç sürmüyordu** (M8-01'in ölçtüğü kalıp). Yeni test seviyeyi
> **`05-config.yaml`'dan okuyup** ürünün kendi `parseLevel`'ından geçiriyor;
> sonucu kritikti çünkü sevk edilen `info` altında `closed` düşerse kapı **kalıcı
> `0/0`** okur ve **her rotasyon adım 3'te durur**. **M-K** (jeton DEĞERLERİ
> yeniden adlandırıldı) — Go tarafındaki her iddia **sembolikti**, README'nin dört
> `grep`'iyle arasında hiçbir şey yoktu; yeni test iki temsili **değere göre**
> bağlıyor ve her jetonun **iki** kapı tablosunda da bulunmasını istiyor.
> **M-B**'nin ortaya çıkardığı **tırnak körlüğü** de kapatıldı (`- name: "X"`).
>
> **RUNBOOK GERÇEĞE ÇEKİLDİ:** *"`50-backup.yaml` her gece dump gönderiyor"* geniş
> zamandaydı ve **ölçtüm: `kubectl -n tappa get cronjob` → `No resources found`**;
> cümle **koşullu mekanizma** kipine çevrildi (*"CronJob çalışıyorsa …; çalışmıyorsa
> bu adım bugün boştur"*) + kontrol komutu. **İki yanlış sınır atfı** (13 → **1**)
> düzeltildi. `:1189` ↔ `:1111` çelişkisi çözüldü (kapı cümlesi artık *"bu depodaki
> ikili"* diyor ve ağaç/küme ayrımını 5c'ye bağlıyor). **`~/.pgpass` temizliği artık
> yorum değil komut** — geçici dosyalar için tam bir imha zinciri varken kalıcı bir
> dosyadaki SUPERUSER parolasını yorumla geçmek asimetriydi. Kartın **"ders NNN"
> satır-numarası atıfları ADA çevrildi** (ders *"satır numarası yazma, adım adını
> yaz"*), kalan çıplak numara **0**.
>
> **HAK TESLİMİ (denetçinin ölçümü):** jeton kapısı 16 log-seviyesi kombinasyonunda
> **hiçbir yanlış cevap** vermedi, yalnız `warn`/`error` altında kullanılamaz
> (`0/0 → DUR`) — güvenli yön; çoklu pod'da tehlikeli yön **kurulamadı**; log
> rotasyonu bu deployment'ta **üretilemez**; karşılıklı dışlama kaçamadı; ve kapının
> **hüküm basmayıp sayması** FAZ C'nin *"kapatma, say"* dersinin doğru uygulaması
> sayıldı.
>
> **🔴 [FAZ F · 4. TUR] İKİ YENİ DENETÇİ DE RED — VE ÜÇ BLOKLAYANIN ÜÇÜ DE 2.
> TURUN KENDİ DÜZELTMELERİNİN ÜSTÜNDEYDİ. SINIF ADLANDIRILDI VE BİÇİM DEĞİŞTİ.**
>
> **ORTAK KUSUR, ve bu bir yama listesi değil bir BİÇİM hatasıydı: eklediğim her
> kapı bir YOKLUĞU okuyordu.** *"OPEN satırı yok"* · *"ad listede yok"*. Bir yokluk
> hiçbir zaman *"kapalı"* kanıtı değildir — *"henüz bakmadım"*, *"log seviyesi
> yükseltilmiş"*, *"yanlış pod"*, *"log dönmüş"* ile **aynı görünür**. Ders **"BİR SINIF DÖRT TUR ÜST ÜSTE ÜRETİYORSA YAMA İSTEME, KARAR İSTE"**'in
> ölçütü uygulandı: kapı **pozitif bir olgu** okumalı ve doğruluğu **savunulmamalı,
> bir komut tarafından çözülmeli**.
>
> **B1 — ADIM 3'ÜN TEK KAPISI ÜÇ KEZDEN İKİSİNDE YANLIŞ CEVAP VERİYORDU.** Uyarı
> açılışta bir, sonra 15 dk'da bir; kapı `--since=5m` okuyordu → doğru cevap oranı
> `5/15 = %33`. Ve zincir daha ağırdı: `20-app.yaml`'da **`reloader`/`checksum`
> annotation'ı yok** (kendi ölçümüm: canlı Deployment'ta `annotations` **boş**),
> yani **sırrı düzenlemek pod'u yeniden başlatmaz**; bölümde *"rollout"* 7 kez
> geçiyordu ve **hiçbiri komut değildi** (`rollout restart` 0, `rollout status` 0)
> — oysa repo bu bilgiyi `externalsecret.example.yaml`'da zaten taşıyordu. Sonuç:
> operatör *"pencere kapandı"* deyip sızmış KEK'in **emanet kopyasını imha
> ediyordu**, süreç o KEK'i kabul etmeye devam ederken ve geri alma kopyası da
> gitmişken.
> **Kapatıldı — BİÇİM DEĞİŞTİRİLEREK:** süreç artık açılışta **iki durumdan tam
> birini** yazıyor (`kek_rotation_window=open` / `=closed`; açık olan ayrıca 15
> dk'da bir nag). Kapı iki sayıyı okuyor ve **üç** sonucu ayırıyor: açık · kapalı ·
> **`0/0` = BİLİNMİYOR, DUR**. `--since` kaldırıldı (satır açılışta düşer).
> `rollout restart` + `rollout status` adım 1 ve adım 3'ün **gövdesine** yazıldı
> (ölçüldü, YALNIZ çalıştırılabilir satırlar: **2 `rollout restart`, 3 `rollout
> status`** — önceki *"2 restart, 5 status"* sayımı düzyazıdaki geçişleri de
> sayıyordu). Ve `0/0` okumasında **hiçbir şey imha edilmiyor**.
> Sabit de pinlendi: `15 * time.Minute` → 30 gün mutasyonu artık **kırmızı**
> (`TestKEKRotationWarnEvery_IsShortEnoughToBeANag`) — 2. turun testi kendi
> `time.Millisecond`'ını atıp sevk edilen değeri hiç sürmüyordu, **M8-01'in ölçtüğü
> kalıbın aynısı**.
>
> **B2 — "SINIFI KAPATAN TEST" BİR DİZGİ ARIYORDU, ENJEKSİYON DEĞİL.** Regex dosyanın
> **tamamında** `- name: X` eşliyordu ve girdinin `value`/`valueFrom` **taşıdığını
> sormuyordu**. İki mutasyon yeşil geçiyordu: `valueFrom` bloğunu **silmek**
> (Kubernetes EnvVar'a `""` verir → `optionalKey32` **nil** → pencere sessizce
> kapalı) ve girdiyi **initContainer'a taşımak** (o konteyner sunucudan önce ölür).
> **Kapatıldı:** parse artık `containers:` altındaki **`tappa` adlı** konteynerin
> `env:` listesine **kapsanmış** ve her girdinin **kaynak taşıdığını** istiyor.
> **M2 ve M2b'nin ikisi de artık KIRMIZI** (kendi ölçümüm). ⚠️ Ve kapsam daraltmasının
> **kendisi** pinsizdi (kapsamı dosyaya geri genişleten mutasyon **yeşildi**): parser
> artık **kendi dejenere manifestleriyle** test ediliyor — initContainer'daki ad ·
> sidecar'daki ad · `ports:` altındaki `- name: http` · kaynaksız girdi · yalnız
> yorumda geçen ad. 🔴 **Ve o test yazılırken parser'ın İLK hâli sıfır girdi
> döndürdü** — `- name: http` konteyner sınırı sanılıyordu — **yakalayan şey testin
> kontrolüydü**. Aracın kendi dejenere girdileri, aracın kendisi kadar önemliymiş.
>
> **B3 — ADIM 1'İN "ROLLOUT İŞE YARADI MI" KAPISI TOTOLOJİYDİ.** Deployment
> **SPEC**'ini okuyordu; girdi koşulsuz ve kalıcı olduğu için sır boşken de yeşildi,
> ve `optional: true` bir **yazım hatasını** (`TAPPA_TAG_KEK_PREV`) sessizce yutuyordu.
> Kapatıldı: o komut kaldırıldı, yerine B1'in pozitif log kapısı geldi — yazım
> hatasında süreç `closed` yazar, tablo *"pencere AÇILMADI"* der ve operatör durur.
>
> **B4 — `optional: true` pinsizdi.** Pinlendi
> (`TestPackaging_TheRotationKEKIsOptionalSoTheSteadyStateStarts`, kontrolü de var:
> `TAPPA_TAG_KEK` **optional olmamalı**). Denetçilerin ayrıldığı yerde güvenlik
> gözü haklıydı ve kartın dili ona göre: bayraksız hâl bir **kesinti değil**,
> `maxUnavailable: 0` sayesinde **takılı bir rollout**.
>
> **UCUZLAR KAPATILDI:** owner parolası artık argv'de değil (`~/.pgpass` + parolasız
> DSN — *"KEK'i argv'den yasaklayıp owner DSN'ini argv'ye koymak"* aynı tehdit
> modeline zıt muameleydi) · `cat >` yerine `read -rs` (etkileşimli tty girdiyi
> **ekrana yankılıyordu**, bir satır yukarıda *"terminale yazdırma"* yazarken) ·
> `20-app.yaml`'ın *"dört"* yorumu **beş** oldu · ADR atfı **md. 3 → md. 5**, ve md.
> 5'in `ChangeKey`'i **MVP dışı** bıraktığı yazıldı · kartın 2. turdaki *"`make
> audit` çıkışı 1'dir"* düzeltmesi **kendisi yanlıştı** (make **2** döner;
> `govulncheck` 1) · `writeAll` yorumu kendi test dosyamla **çelişiyordu**
> (`1>&-` bugün de exit 0 verir **ve** yazar: Go runtime kapalı standart fd'lere
> `/dev/null` açıyor) — yorum düzeltildi, testin ölçümü doğruydu.
>
> **YENİ SAYILMIŞ AÇIKLAR:** rotasyonun **`audit_log`'da izi yok** (araç bir filtre,
> yazan `psql`) — *"kaç satır sızmış KEK altında kaldı"* sorusunun **ürün içinde
> cevabı yok**; `TAPPA_ROTATE_ALLOW_UNOPENABLE`'ı *"bir kayıttır"* diyen cümle
> **indirildi**, artık yalnız *"hız kesici"* · iki-KEK yolunun **son kullanma
> tarihi/sayacı/göstergesi yok** · ön koşul 5b BYPASSRLS oturumunun **kapsamı,
> ömrü ve izsizliği** yazıldı · `tappa-secrets` **elle** yönetiliyor (ölçtüm: 23 ESO
> CRD kurulu, `tappa` namespace'inde **0** ExternalSecret) · ve 🔴 **canlı Deployment
> bu değişikliğin ÖNCESİNDE**: env listesi hâlâ **dört** ad taşıyor ve çalışan ikili
> `kek_rotation_window=` satırını yazmıyor, yani kapı ancak manifest + imaj sevk
> edildikten sonra anlamlı — ön koşul 5c bunu ölçümüyle söylüyor.
>
> **🔴 [FAZ F · 2. TUR] İKİ DENETÇİ DE RED. ÜÇ BLOKLAYAN, VE 1. TURUN RAPORU
> BUNLARDAN İKİSİNİ *"kapandı"* DİYE YAZMIŞTI.**
>
> **F1 — RUNBOOK'UN 1. ADIMI ÜRETİMDE HİÇBİR ŞEY YAPMIYORDU.** `20-app.yaml` sırrı
> **anahtar anahtar** çekiyor (`envFrom` bilinçli olarak kullanılmıyor) ve listede
> **dört** ad vardı. 1. turun diff'i üç manifeste de dokunmadı. Zincir: operatör
> sırra ekler → rollout **yeşil** → değişken pod'a **hiç** enjekte edilmez →
> `config.Load` boş okur → süreç **tek** KEK tutar → adım 2 parkı sunucunun
> tutmadığı anahtarın altına taşır → **her plakete her tap 500, hiç işlem satırı
> yazılmadan** (§4.6), ve **geri alma yolu da aynı eksik enjeksiyona bağlı**.
> Kapatıldı: beşinci `secretKeyRef` (`optional: true`) + iki örnek manifest +
> **sınıfı kapatan test** — `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest`,
> `internal/config`'in okuduğu **her** adın bir manifestte bulunmasını istiyor
> (manifest satırı silinince **kırmızı**, ölçüldü). Testin kendi kontrolü de var:
> `DATABASE_MIGRATE_URL` `20-app.yaml`'da **yalnız yorumda** geçtiği için, yorum
> ayıklayıcı bozulursa ikinci test kırmızı veriyor — çıplak bir `grep` onu
> *"enjekte ediliyor"* sanırdı.
>
> **F2 — SON KOŞUL YANLIŞ ÖZELLİĞİ ADLANDIRIYORDU, VE 1. TURUN "KANITI" DA O
> YÜZDEN EKSİKTİ.** Gerekçe *"yazan oturum `tappa_owner`'dır, sayısı doğru
> olandır"* diyordu. Sayımı doğru yapan **owner olmak değil, RLS'i atlamaktır**:
> `tags` FORCE RLS taşıyor ve politikası **PUBLIC**'e yazılı (`polroles={0}`), yani
> `NOSUPERUSER NOBYPASSRLS` bir sahip de filtrelenir. 1. tur *daraltılmış okuma +
> **süper** yazıcı*'yı ölçmüştü; ölçülmeyen yol *daraltılmış okuma + **daraltılmış**
> yazıcı*'ydı — **ve runbook tek `$OWNER_DSN` ile tam o kurulumu üretiyordu.**
> Kendi yeniden üretimim, 73 100 satırlık kopyada:
> ```
> has_column_privilege(...,'aes_key_ref','UPDATE')  ->  t     (runbook'un kapısı "doğru rol" diyor)
> SELECT count(*) FROM tags                          ->  0     (aynı rol parkı HİÇ görmüyor)
> araç                                               ->  EXIT 0: every row read is accounted for
> psql                                               ->  re-sealed 15 rows, COMMIT, exit 0
> gerçek                                             ->  73 100'ün 15'i döndü; %99,98'i sızmış KEK altında
> ```
> Kapatıldı: üretilen SQL artık saymadan **önce oturumu bağlıyor** —
> `app.tenant_id` set edilmiş olamaz **ve** `rolsuper OR rolbypassrls` doğru olmalı;
> ikisi de ayrı ayrı `RAISE EXCEPTION`. Aynı repro artık iki farklı `ERROR` ile
> düşüyor, **0 satır yazılarak**. ⚠️ Ders **"DÜNYA HAKKINDA İDDİA BAYATLAR, MEKANİZMA HAKKINDA İDDİA BAYATLAYAMAZ"** uygulandı: iddia **dünya** hakkında
> olmaktan (*"yazan oturum owner'dır"*) **mekanizma** hakkında olmaya geçti
> (*"oturum RLS'i gerçekten atlıyor, yoksa reddediyorum"*).
>
> **F3 — AÇIK ROTASYON PENCERESİNİN SIFIR SİNYALİ VARDI.** Kapatıldı: açık pencere
> **her 15 dakikada** bir `slog.Warn` yazıyor (anahtar değil, **olgu**). ⚠️ Ve
> denetçinin önerdiği `/readyz` **bilinçli olarak seçilmedi**: o uç nokta
> kimliksizdir ve gövdesi *"hiçbir sebep adlandırmaz"* diye belgelidir; *"bir KEK
> rotasyonu sürüyor"*, tam olarak **sızmış KEK'i elinde tutana** yarayan bilgidir
> (anahtarının hâlâ canlı olduğunu söyler). Sapma ve gerekçesi kodda yazılı.
>
> **YEŞİL KALAN MUTASYONLAR — denetçinin 6'sı, benim sonucum.** B2 (çıkış kodu
> sabitleri kendi kendilerine iddia ediliyordu) · T1 (§4.5 tenant kemeri) ·
> T6 (`sun.Zero`) · T5 (`bufio` gerekçesi) **pinlendi**; F2/F3/F6/F1 için yeni
> pinler eklendi ve **hepsi kırmızı** doğrulandı. **T2 BİLEREK YEŞİL BIRAKILDI:**
> `UnwrapAny`'nin sırası *"load-bearing"* diye yazılmıştı ve aynı yorum iki satır
> altta *"a cost decision, not a security one"* diyordu. Ölçüm ikincisini
> doğruluyor — bir ref **tam olarak bir** anahtarla açılır, yani döngüyü ters
> çevirmek hiçbir sonucu değiştirmez. **Pinlemek yerine İDDİA DÜZELTİLDİ** (ders
> **"BİR YORUMUN İLAN ETTİĞİ ÖZELLİĞİ KODUN TAŞIDIĞINI DOĞRULA"**); yeşil mutasyon
> artık bir boşluk değil, doğru cevap.
>
> **1. TURUN ÜÇ CÜMLESİ DÜZELTİLDİ:** (a) yukarıdaki *"RLS'in DARALTMASI …
> yakalanıyor"* ölçümü **daraltılmış okuyucu + süper yazıcı** kurulumuna aitti ve
> runbook o kurulumu üretmiyordu — artık iki kurulum da tabloda, ayrı satırlarda;
> (b) `make audit`'in **kendi** çıkışı **2**'dir (make'in
> başarısız recipe kuralı); **1** olan `govulncheck`'tir — özet satır
> `govulncheck exit=1 - redline-check exit=0`. ⚠️ Bu maddenin 2. turdaki hâli
> *"çıkış 1'dir, 2 değil"* diyordu, yani **bir düzeltme cümlesi düzelttiğini iddia
> ettiği sayıyı yanlış yazdı**; ölçüm: `make audit; echo $?` → **2**; (c) *"adım 3'ün mekanik kapısı yok"* hâlâ doğru, ama
> artık **gözlemlenebilir** — sessiz değil.
>
> **YENİ SAYILMIŞ AÇIKLAR (sınır 9'a yazıldı):** rotasyon **yedekleri döndürmez**
> — rotasyon öncesi her dump plaket anahtarını sızmış KEK altında taşır, yani
> *"sızmış KEK + eski dump = parkın tamamı"*, rotasyondan sonra da; runbook'a
> **adım 4** (imha **ya da** `BACKUP_RETENTION_DAYS` boyunca sızmış KEK'i canlı
> say) eklendi ama bu kümede yedek CronJob'ı yok, yani **hiç koşulmadı**. Ayrıca
> **sunucu tarafı ifade log'u**: geliştirme Postgres'i 21 satırlık koşularda
> **105 sarmalı ref literalini** log'a yazdı (üretim `args: []` ile temiz —
> *tesadüfen*), ön koşul 6 bunu kapatmayı **adım** yapıyor. Ve
> **`cmd/rotatekek` hiçbir imajda yok**.
>
> **Orkestratörün ölçümlerinden ÇÜRÜTÜLEN bir madde:** *"üretimde `tappa_owner` süper
> değilse FORCE RLS onu da bağlar"* endişesi ölçülerek daraldı —
> `k8s/10-postgres.yaml` `POSTGRES_USER: tappa_owner` veriyor ve `postgres` imajının
> giriş betiği `POSTGRES_USER`'ı **initdb'nin bootstrap süper kullanıcısı** yapar. Taze
> bir `postgres:17-alpine` konteynerinde aynı değişkenle ölçüldü: **`rolsuper=true`,
> `rolbypassrls=true`**. Bu bir **mekanizma** kanıtıdır; üretimde `SELECT rolsuper`
> **koşulmadı** ve koşulmadığı runbook'ta yazılı.
>
> ⚠️ **VE 2. TUR BU MADDEYİ KISMEN GERİ ALDI: orkestratörün endişesi ÇÜRÜK
> DEĞİLMİŞ, YALNIZCA YANLIŞ YERE BAKIYORMUŞ.** *"Süper olmayan bir rolde FORCE RLS
> ısırır"* cümlesi **doğruydu**; yanlış olan, onu yalnız **üretimdeki
> `tappa_owner`'ın kimliği** sorusu sanmamdı. Gerçek isabet noktası
> **prosedürün kendisiydi**: runbook tek `$OWNER_DSN` ile okuyup yazıyordu, yani
> süper olmayan **herhangi** bir rol (ve §1'in hedefi olan **managed
> Postgres**'te owner çoğu zaman süper **değildir**) sessiz kısmi rotasyon
> üretiyordu. Bir endişeyi *"o değişken şu değeri alıyor"* diye kapatmak, endişenin
> **sınıfını** kapatmaz.

---

## M8-03 — Gözlemlenebilirlik

- **Bağımlılık:** M8-02
- **Kırmızı çizgi:** §4.7 · §7
- **Commit:** `feat(ops): add structured logging and alerts`

**Kabul kriterleri.**
- `log/slog`, yapılandırılmış, request id korelasyonlu.
- **Asla loglanmayanlar** doğrulandı: oturum token'ı, `token_hash`, CMAC, AES
  anahtarı, davet kodu, tam GPS koordinatı.
- Uyarı kuralları: `reject` oranı sıçraması, `flag` kuyruğu birikmesi, deaktive
  hesap denemesi, şüpheli ctr sıçraması, 5xx oranı.
- Prod log'ları AB'de, saklama süresi belirli.

> **Kart düzeltmesi (2026-08-19, M8-03 uygulaması sırasında).** Dört kriterin
> **ikisi kartın yazdığı şekilde sevk edilemiyordu** ve bu ölçülerek bulundu; ikisi
> sevk edildi. Kart bunları saymadığı için burada sayılıyor.
>
> **1. "request id korelasyonlu" — KAPSAM DARALTILDI, GEREKÇESİYLE.**
> `middleware.RequestID` M5-03'ten beri bağlı ve **hiçbir şey onu okumuyordu**
> (üretim ağacında `RequestID|request_id|requestID|X-Request-Id` → **tek isabet**,
> o da onu üreten satır). Üç şekil ölçüldü, maliyetleri raporda; seçilen
> `slog.Handler` sarmalayıcısıdır ve o **yalnız `*Context` çağrılarını**
> damgalayabilir. **Kart öncesi** ağaçta **346** logger çağrı yeri vardı (2. turda
> yeniden ölçüldü — bu blok bir tur boyunca 347 yazdı ve `deploy/README.md` aynı
> olguya 346 diyordu); hepsini dönüştürmek backlog **T51**'in ölçüp reddettiği
> paket çapında dönüşümdür (`internal/handler`'da 224).
> **Sevk edilen: 32 çağrı yeri (tap karar zinciri) + her yanıt için bir
> `http.request` kaydı.** Geri kalanı korele **edilemez** ve bu `deploy/README.md`
> **sınır 26**'da sayıldı.
>
> **2. "asla loglanmayanlar doğrulandı" — ALTI SINIFTAN BİRİNİN HİÇBİR MEKANİZMASI
> YOKTU.** Ölçüldü: bir log çağrısına `"latitude", 35.8997` eklemek
> `scripts/redline-check.sh`'i **exit 0** bırakıyordu — R7'nin deseni koordinatla
> ilgili tek bir ifade taşımıyor.
>
> ⚠️ **[GERİ ÇEKİLDİ — bu paragrafın kalanındaki üç cümle de sevk edilen kodun
> TERSİNİ söylüyor. Doğrusu aşağıdaki *"3.–5. tur"* bloğundadır; iki cümleyi
> birden okuyacaksan aşağıdakine güven.]** Geri çekilen hâli şunu diyordu:
> *"Eklenen: `geo.Point.LogValue` + `tenant.GPS.LogValue` (tip düzeyi) ve yeni R7c
> kuralı. Ayrıca R7b/R7c'nin eşleşme penceresi bir seviye dengeli parantez
> taşıyacak şekilde genişletildi (temiz ağaçta 0 yanlış pozitif); **R7
> genişletilmedi**. 2. turda bu cümle düzeltildi: 'aynı genişletme' iki ayrı şeydi
> (dengeli parantez penceresi ve `rg -U`), **R7'de ikisi de yok**, ve ölçüldü —
> yalnız pencere: 1 yanlış pozitif, pencere + `-U`: 3."*
> Üç cümlenin üçü de 4. turda geçersizleşti: her iki tipte **birer değil BEŞER**
> metot var, R7 **genişletildi**, ve R7'de bugün **ikisi de** var. Ölçümler ve
> ne kaldığı aşağıda. `deploy/README.md` bunu zaten *"R7 GENİŞLETİLDİ … ve önceki
> turda 'yapılmadı' diye yazılması BİR HATAYDI"* diye yazıyor; iki belge artık
> aynı şeyi söylüyor.
>
> **3. "uyarı kuralları" — TESLİMAT KANALI YOK, HESAPLANABİLİRLİK SEVK EDİLDİ.**
> Altı sinyalin **hiçbirinin filtrelenebilir bir olayı yoktu:** karar motoru bugüne
> kadar **tek bir verdict satırı bile loglamıyordu**, 5xx için erişim kaydı yoktu, ve
> hazırlık kaybı bir **düzyazı cümlesi** olarak yazılıyordu (`"readiness lost: the
> database did not answer…"`) — yani bir kural onu bir olay adıyla arayamazdı. Eklendi:
> `tap.decision` (1., 2. ve 4. sinyal), `tap.security_alert` (3.),
> `http.request` (5.), `readiness.lost`/`readiness.regained` (6.). Eşikler ve
> sorgular `deploy/README.md` → *"M8-03 — UYARI
> KURALLARI"*. **Gönderim yok** — kararı `Q28 (a)` (**teslimat**: uyarı nereye
> gidecek), ve bu **sınır 25**'te yazılı. ⚠️ Bu cümle bir tur boyunca *"Q12 açık"*
> diyordu; `Q12` **barındırmadır** ve uyarı hedefi hakkında hiçbir şey söylemez —
> yani devir hedefi **var olmayan bir karardı**. `Q28` tam bu ölçüm üzerine açıldı
> (2. tur denetimi, 2026-08-19). Saklama yarısı `Q28 (b)`'dir ve 4. maddededir.
>
> **4. "prod log'ları AB'de, saklama süresi belirli" — AB YARISI DOĞRU, SAKLAMA
> SÜRESİ YARIM.** Node **fsn1 (Almanya)**; ⚠️ ve `topology.kubernetes.io/region`
> etiketi **yok** (ölçüldü), yani bölge kanıtı node ADI. **İkinci bir kopya
> bulundu ve kart bilmiyordu:** SigNoz otel ajanı `/var/log/pods/*/*/*.log`
> topluyor ve exclude listesi `tappa`'yı **saymıyor** — ClickHouse aynı node'da,
> yani AB'den çıkmıyor. Saklama: node'da `10Mi × 5 = 50 MiB` (**boyut, süre
> değil**) ve fiilen *"bir sonraki deploy'a kadar"* (7 ReplicaSet, **1** pod).
> **SigNoz kopyasının TTL'i DOĞRULANAMADI** — `exec`/API gerekiyordu, bu turda
> yalnız salt-okuma fiilleri kullanıldı. Operatör eylemleri adlandırıldı
> (`log-retention-kubelet`, `log-retention-signoz`) ve M8-06 pilot kapısına bağlandı.
> Kararın kendisi `Q28 (b)` — **saklama** yarısı. ⚠️ `Q12` (barındırma) buna
> **dolaylı** bağlıdır (nerede barındığımız node rotasyonunu ve toplayıcının
> varlığını belirler), ama *"ne kadar saklanacak"* sorusu `Q12` değildir.
>
> **Kart düzeltmesi — 2. TUR (2026-08-19, üçüncü göz RED verdikten sonra).**
> Denetim beş bloklayan buldu; üçünün **tek bir kökü** vardı ve o kök burada
> yazılıyor, çünkü kart bunu da saymamıştı.
>
> 🔴 **KÖK: erişim kaydı SAĞLIK SONDALARINI da kapsıyordu**, ve bu üç ayrı şeyi
> aynı anda bozuyordu. (a) `Handle`'ı 2 sn uyutan bir log hedefiyle `GET /healthz`
> **2,001 sn** sürüyordu — yani canlılık sondası log borusuna bağlanmıştı ve
> boşaltılamayan bir boru kubelet'e **sağlıklı süreci öldürtüyordu**; tam olarak
> `TestHealthz_CannotDependOnAnything`'in adını taşıdığı arıza, gözlemlenebilirlik
> kartının eliyle geri gelmiş. (b) `/readyz`'in **tasarlanmış** 503'ü `level=ERROR`
> yazıyordu, kubelet onu 5 sn'de bir yokladığı için 5 dakikada **60 olay** oluyordu
> ve *"sunucu bozuk"* kuralının eşiği **5**'ti — geçici bir DB dalgalanması ~25
> saniyede yanlış alarmı çalıyordu. (c) sıfır kullanıcı trafiğiyle **25.920
> istek/gün × ~197 B ≈ 5,1 MB/gün**, yani log'un ~%100'ü sonda.
>
> **Karar:** bir sonda **tasarlandığı gibi** cevap veriyorsa (`/healthz`→200,
> `/readyz`→200 **ve** 503) erişim kaydı **yazılmaz**; **tasarım dışı** her durum
> normal şekilde kaydedilir — `/readyz`'den gelen bir **500** hâlâ `level=ERROR`
> ile 5. kurala düşer, yani paniklemiş bir hazırlık handler'ı sessiz kalmaz.
> **Tam dışlama seçilmedi** tam da bu yüzden. Ve gerçek bir hazırlık arızası
> görünür kalıyor: `internal/handler.Health` durum değişimini zaten yazıyordu —
> artık `readiness.lost` / `readiness.regained` adıyla pinli ve **6. uyarı kuralı**
> onu okuyor. Kayıt kaybolmadı, **yeri değişti** (§4.6) ve sayısı 12/dakikadan
> olay başına 1'e indi.
>
> 🔴 **İKİNCİ DAVRANIŞ HATASI, aynı yerden:** `AccessLog` `Recoverer`'ın **dışında**
> (bu bilinçli — panik 500 olarak kaydedilsin diye), dolayısıyla **kendi** paniğini
> kimse yakalamıyordu. Ölçüldü: paniklerken `Handle` ile `/healthz` **ve** sıradan
> bir rota **`EOF`** dönüyordu; yığın `net/http.(*conn).serve`'de bitiyordu,
> `Recoverer`'da değil. `writeAccessRecord` artık kendi paniğini kapsıyor ve bunun
> handler paniğini **yutmadığı** ölçüldü (Go'nun kuralı burada sezgiye aykırı:
> normal dönen bir yardımcının `defer`'indeki `recover`, dışarıda süren bir paniği
> durdurmaz — üç kombinasyon da sondalandı). `TestAccessLog_PanicIsRecordedAsA5xx`
> yerinde duruyor.
>
> **Metin tarafında düzeltilenler:** uyarı tablosundaki **olmayan bir sid**
> (`…tag-lost`; gerçeği `sys:tag-not-active`) — düzeltildi **ve** bölümdeki her
> sid'in `internal/policy`'de var olduğunu doğrulayan bir test kondu ·
> *"Kabul edilmiş sınırlar"* listesindeki **17/18/19 çiftlenmesi** → 24/25/26 ve
> numaraların tekilliği artık testle tutuluyor · saklama hesabı **M8-03 ÖNCESİ**
> bir ikilinin log'u üzerinden yapılmıştı (o pod'da `http.request` sayısı **0**),
> yeniden hesaplandı · `tap.security_alert`'in **ikinci yolu** (kayıp plaket)
> belgeye yazıldı ve testle sürüldü · toplayıcıya giden **üç** konteynerin
> §4.7 kapsamı sayıldı · `slog.Default()` (44 → **23** + dışarıda 20) ve logger
> çağrı yeri (347 → **346**, ağacı adıyla) sayıları düzeltildi.
>
> **Ek olarak kapatılan iki backlog borcu:** **T47** (rotasyonun `lock_timeout`
> başlığı bir TAP tavanı ilan ediyordu — düzeltildi; `55P03` runbook'a girdi;
> `defer sun.Zero` için var olduğu iddia edilen test **yazıldı** ve mutasyonla
> kanıtlandı) ve **T49** (`LD_PRELOAD`/`DYLD_*` temizleme listesine eklendi; DSN'in
> URI biçimi zorunlu kılındı; yanlış kanal sayımı kaldırıldı).

> **Kart düzeltmesi — 3.–5. TUR (2026-08-19).** Bu kart, yukarıdaki 2. tur bloğuna
> kadar yazılmıştı ve **sonraki üç turda sevk edilenlerin hiçbirini saymıyordu**;
> ölçüldü: `git diff --stat docs/plan/m8-deploy-pilot.md` = **108 ekleme, hepsi 2.
> tur bloğuna kadar**. Sonuç, kartın sevk edilen kodun **tersini** söylemesiydi.
> Bir sonraki oturumun ilk okuduğu belge bu olduğu için, eksik olan burada
> sayılıyor.
>
> 🔴 **KÖK DÜZELTME — R7 GENİŞLETİLDİ (4. tur), ve kartın *"genişletilmedi"*
> demesi bir hataydı.** R7 §4.7'nin **en sert beş sınıfını** taşıyor (oturum
> token'ı · `token_hash` · CMAC · AES anahtarı · davet kodu) ve R7b/R7c'ye ödenen
> bedelin **ikisini de** ödememişti: `-U` yoktu, pencere ilk `)` karakterinde
> duruyordu. Bir güvenlik denetçisi beş sınıfın **beşini de** tek yazımla geçirdi —
> tetikleyiciyi `id.EmployeeID()` gibi bir çağrının **arkasına** koymak yetti, ki bu
> tap karar kaydının **bugünkü** yazımıdır. Bugün R7'de **hem `-U` hem dengeli
> parantez penceresi** var. Ölçülen bedel: eski hâli **0** yanlış pozitif (ama beş
> sınıfın beşi de kaçıyordu) · yalnız pencere **1** · pencere + `-U` **3** (sevk
> edilen). Üçü de `R7_WAIVERS`'ta **birebir adla** muaf ve **her koşuda WARN** olarak
> basılıyor. Adları ve gerekçeleri: `scripts/redline-check.sh` ve
> `deploy/README.md` §4.7 bölümü.
> **Kanıt (5. turda yeniden koşuldu):** aynı sızıntı sevk edilen R7 ile **exit 1**,
> kart öncesi desenle **exit 0**.
>
> 🔴 **İKİNCİ KÖK DÜZELTME — GPS redaksiyonu BİRER değil BEŞER metot.**
> Kart *"`geo.Point.LogValue` + `tenant.GPS.LogValue`"* diyordu; sevk edilen
> `geo.Point` ve `tenant.GPS` tiplerinin **her birinde beş** metot var:
> `Format` · `String` · `GoString` · `LogValue` · `MarshalText`. 4. turda ölçüldü:
> yalnız `LogValue` ile `%v` · `%+v` · `%#v` · işaretçi üzerinde `%v` ·
> `json.Marshal` · **değerle bir struct alanının içinde** · `[]Point` ·
> `map[string]Point` — sekizi de tam koordinatı basıyordu, ve ikisi **iki ağı
> birden** deliyordu (R7c bir eksen **adı** arar, o iki yazımda yok). Kalan tek
> delik yazılı ve testle sabitlenmiş: başka bir struct'ın **dışa açık olmayan**
> alanındaki bir `Point`, `%v` altında hâlâ sızar — `fmt` orada `Formatter`'a hiç
> danışmaz.
>
> **Kartta HİÇ yazılı olmayan ve sevk edilen altı şey daha:**
> 1. **Kendi `RequestID` middleware'i** — chi'nin `middleware.RequestID`'si gelen
>    `X-Request-Id` başlığını **olduğu gibi**, uzunluk ve karakter sınırı olmadan
>    kullanıyordu; bu kart o alanı **her** erişim kaydına bağladığı için 900 KB'lık
>    bir başlık 921 757 baytlık bir log satırı üretiyordu (ölçüldü: 30 kimliksiz
>    istek = 26,4 MiB). Gelen değer artık **sınırlandırılıyor**, reddedilmiyor.
> 2. **`MaxHeaderBytes` = 16 KiB** (Go'nun varsayılanı 1 MiB idi) — tek başına (1)'i
>    kapatmaz, kural yazılmamış **başka** başlık kanallarını sınırlar. ⚠️ Bu tavanı
>    aşan istek başlık ayrıştırma sırasında 431 ile kesilir ve **hiç erişim kaydı
>    üretmez** (ölçüldü: 0 bayt) — `deploy/README.md`'de sınır olarak yazılı.
> 3. **Beşli redaksiyon deseni** (yukarıdaki ikinci kök düzeltme).
> 4. **R7 genişletmesi + üç muafiyet** (yukarıdaki kök düzeltme).
> 5. **`rotate-kek` Go toolchain listesi + envanter testi** — temizleme listesine
>    `GOFLAGS`/`GOTOOLCHAIN`/`GOENV`/`GOROOT`/`GOCACHEPROG`/`GOTOOLDIR` eklendi, ve
>    liste artık elle değil `go env`'in **kendi** sayımına karşı doğrulanıyor
>    (`TestRotateScript_AccountsForEveryGoToolchainVariable`). Bu test 4. turda
>    `GOFLAGS`'ı, 5. turda `GOROOT`'u yakaladı — yani ağ, yazıldıktan sonra iki kez
>    iş gördü.
> 6. **Logger çağrı yerinin adlandırılmasının invaryanta çevrilmesi** —
>    `TestObservability_EveryLoggerCallSiteIsSpelledLog`: R7/R7b/R7c yalnız alıcısı
>    `log`/`slog`/`fmt` yazılmış bir çağrıyı görüyor, ve bu ağaçta **kaza eseri**
>    hepsi öyle yazılmıştı. Artık kaza değil.
>
> **Sayılar (5. turda yeniden ölçüldü, AST):** kart öncesi ağaç **346** logger çağrı
> yeri / 46 dosya / **0** `*Context`; kart sonrası **349** / 47 dosya / **32**
> `*Context`. Dönüştürülen **31** + yeni **1** = 32. ⚠️ `deploy/README.md` **sınır
> 26** bir tur boyunca **33** dedi (dökümü `internal/handler/health.go`'ya 2
> veriyordu; o dosyadaki iki kayıttan biri `LogAttrs`'tır ve `LogAttrs` `*Context`
> **değildir**) — 5. turda **32**'ye indirildi. Çok satırlı çağrı yeri:
> kart öncesi **137**/346, kart sonrası **141**/349; *"131"* yeniden üretilemedi ve
> geri çekildi.
>
> ⚠️ **Bu tur yalnızca METİN düzeltti** — hiçbir `.go` davranışı değişmedi, yalnız
> yorum metni ve `.md`/`.sh` yorumları. Bu kart artık `deploy/README.md` ile aynı
> hikâyeyi anlatıyor; ikisi çeliştiğinde **ölçüm komutu yazılı olan** kazanır.

---

## M8-04 — Güvenlik denetimi

- **Bağımlılık:** M8-01
- **Kırmızı çizgi:** §4 (tamamı)
- **Commit:** `chore(security): address pre-pilot audit findings`

> **Kart düzeltmesi (2026-08-19, M8-04 FAZ B1 uygulaması sırasında).** Kart tek
> parça yazılmıştı; **FAZ A** (denetim) bulguları iki farklı denetim merceği
> istiyor — veri katmanı (migration · RLS · GRANT · indeks) ile uygulama katmanı
> (handler · ekran) — bu yüzden düzeltme fazı **B1 (veri katmanı)** ve **B2
> (uygulama katmanı)** olarak bölündü (agent-brief'in mercek ölçütü).
>
> **FAZ B1'in kapsadığı beş bulgu ve sevk edilen mekanizma — hepsi `00021`:**
> **F2 (YÜKSEK)** altı append-only tabloda `BEFORE TRUNCATE ... FOR EACH
> STATEMENT`; ölçüldü ki bir CASCADE zincirinde **çocuğun** tetikleyicisi de
> ateşliyor, yani `TRUNCATE tenants CASCADE` artık adını hiç anmadığı
> `transactions` tarafından reddediliyor · **T7** `tags_aes_key_ref_is_kek_envelope`
> (`octet_length = 44`, ADR 0003 md. 4) — 61 033 satır ihlal ettiği için
> **`NOT VALID`**, ve fikstürlerle `seed.sql` yer değiştiren yer tutucuya
> geçirildi · **T15** `tags_uid_canonical_hex` **VALIDATE** edildi (küçük harfli
> satır: **0**, ölçüldü) · **T17** `audit_log (tenant_id, target)` — Bitmap Heap
> Scan → Index Scan, 1 216 → 6 buffer · **T22** `make db-reset` artık
> `goose reset` değil: `scripts/db-reset.sh` konteyneri ve volume'u silip
> `db-init`'i yeniden koşturuyor (gerekçe ve elenen iki alternatif o dosyanın
> başlığında; `00013`'ün `Down`'ı §6 gereği **değiştirilmedi**).
>
> ⚠️ **T7'nin panel yarısı FAZ B1'e ait DEĞİL** ve açık kalıyor: `plaques.go`
> hâlâ *"Encoded by Tappa"* etiketini `location_id`'ye bakarak basıyor, anahtara
> değil. Şema artık şekli zorluyor, **ekranın cümlesi** B2'nin işi.

> **Kart düzeltmesi (2026-08-19, M8-04 FAZ B2 2. turu sırasında).**
>
> 🔴 **FAZ A'NIN BULGU LİSTESİ (F1–F8) HİÇBİR YERE YAZILMAMIŞ, VE BU ÖLÇÜLDÜ.**
> Kartın kabul kriteri *"ORTA/DÜŞÜK olanlar ya kapandı ya gerekçesiyle kabul edildi
> **ve yazıldı**"* diyor; o kriter **F7 için sağlanamıyor, çünkü F7'nin ne olduğu
> repoda yazmıyor**.
>
> 🔴 **VE BU BLOĞUN İLK HÂLİ ARAMASINI YANLIŞ RAPORLADI. ÜÇ TUR BOYUNCA.**
> 1. hâli *"`F2`–`F7` hiçbir dosyada geçmiyor"* diyordu; 2. ve 3. hâlleri onu bir
> **sayı tablosuyla** düzeltmeye çalıştı ve **her ikisi de yanlış çıktı** (3. tur
> `F1`=15 `F4`=4 `F5`=4 yazıyordu; yeniden ölçüldüğünde **F1=14, F4=5, F5=5** —
> fazlalar o turda hiç dokunulmamış Go dosyalarındaydı).
>
> 🔴 **BU YÜZDEN SAYI TABLOSU SİLİNDİ, DÜZELTİLMEDİ (4. tur).** Üç kez yanlış çıkan
> bir sayı, sayının kendisinin **taşınmaması** gerektiğini kanıtlar: hiçbir kapı onu
> korumuyor, ağaç her commit'te değişiyor, ve okur **yanlış** bir sayıya güveniyor.
> `D19`'un kuralı — *bir sayı ya bağlanır, ya tarihlenir, ya silinir* — burada
> **silinir** tarafına düşüyor. Kalan üç şey:
>
> **(a) SONUÇ, ki karar bunun üzerine kuruluydu:** ağaçtaki `F<n>` etiketlerinin
> **hiçbiri M8-04 FAZ A'ya bağlanmıyor**. Hepsi başka görevlerin bulgu numaraları
> (`docs/plan/m6-dashboard.md`'nin F2/F7'leri, `docs/backlog.md` T32'nin
> *"M7-03 A güvenlik denetimi (F2)"* atfı, `internal/handler/employeeadd_test.go`'nun
> kendi F7'si). Bu görevle **ilişkilendirilebilen tek iki kayıt** şunlardır ve ikisi
> de tek satırdır: `Makefile`'ın *"M8-04 F1..F8 turu"* satırı (aralığın tamamına atıf,
> tek bir bulguya değil) ve `scripts/redline-check.sh`'in R4 bloğundaki `F8` atfı.
> ⚠️ 3. turun bu cümlesi de yanlıştı: `redline-check.sh` **`F1`'i hiç içermiyor**,
> yani *"bağlayabilen tek iki yer redline-check.sh + Makefile"* ifadesinin
> `redline-check.sh` yarısı `F1` için **boştaydı**.
>
> **(b) ONU ÜRETEN KOMUT**, sayının yerine:
> ```sh
> for t in F1 F2 F3 F4 F5 F6 F7 F8; do printf '%s %s\n' "$t" "$(rg -l -w "$t" . -g '!.git' | wc -l)"; done
> rg -n -w 'F1|F8' Makefile scripts/redline-check.sh
> ```
>
> **(c) ⚠️ VE BU KARTIN KENDİSİ `F1`'İ İKİ KEZ, İKİ FARKLI ŞEY OLARAK TANIMLIYOR.**
> Yukarıda (M8-02 FAZ F bloğu) *"F1 — RUNBOOK'UN 1. ADIMI ÜRETİMDE HİÇBİR ŞEY
> YAPMIYORDU"*; aşağıda *"F1 = veritabanı rolü açılış kapısı"*. İkisi **ayrı fazların
> ayrı numaralandırmaları**dır ve kart bunu hiçbir yerde söylemiyordu. Bir `F<n>`
> okunurken **hangi fazın listesinden geldiği** yazılmadıkça belirsizdir; bu, sayı
> tablosunun neden yanlış olduğunun da yarısıdır.
>
> ⚠️ **İddianın özü hiç değişmedi, ölçümü üç kez değişti** — ve fark önemli:
> *"hiç geçmiyor"* bir sonraki okuru **aramaktan alıkoyar**, oysa aranacak şey vardır
> ve **yanlış göreve aittir**. Liste orkestratörün oturumunda kaldı ve bağlam
> sıkıştığında kayboldu — `agent-brief.md`'nin *"kalıcı olmak zorunda"* dersinin bu
> görevdeki tekrarı.
>
> **Bugün doğrulanabilen ne varsa, etiketiyle:** F1 = veritabanı rolü açılış kapısı
> (§4.5; `internal/db`'nin `New`'i üretimde ayrıcalıklı rolü reddediyor) ·
> F8 = R4 deseninin tek yöne körlüğü (`ctr > tag.LastCtr` sessizdi). FAZ B2'nin
> sevk ettiği geri kalan değişiklikler **F numarası değil backlog numarası**
> taşıyor: **T3** (`__Host-` çerez öneki — yeniden tartıldı, **alınmadı**, üç ölçüm
> `internal/handler/cookies.go`'da) · **T23** (`base:gps-conflict-review`'in
> müşteriye basılan gerekçesi yanlıştı) · **T37** (saat dilimi düzenlemesi ilk
> ücretli ayı kaydırıyordu) · **T40** (`/0` aralığı sahte IP kanıtı üretiyordu).
> Ayrıca `make test`/`test-short`/`cover` artık DB env'i olmadan **koşmuyor**
> (öncesinde DB testlerinin **tamamı** sessizce atlanıyor ve `go test` yine exit 0
> veriyordu). ⚠️ **Buraya bir SAYI yazılmıyor, bilinçli olarak:** bu turda yazılan
> her SKIP sayısı **aynı ağaçta** bayatladı, çünkü sayıyı ölçtükten sonra test
> eklendi ve yeniden ölçülmedi. Bir sayıyı hiçbir kapı korumuyorsa ya **bağlanır**
> ya **tarihiyle** yazılır ya **silinir**; kararı taşıyan olgu *"sıfırdan çok"*tur,
> basamakları değil. Güncel değeri üreten komut: `.env`'siz `go test -count=1 -v
> ./... | grep -cE '^[[:space:]]*--- SKIP'`. 🔴 **Çapa girintiyi kabul etmek
> ZORUNDA, yoksa alt testler sayılmıyor:** `go test -v` alt test satırlarını
> girintili yazar, `^--- SKIP` yalnız üst düzey fonksiyonları yakalar. Ölçüldü
> (2026-08-20, aynı ağaç): `^--- SKIP` → 469, girintiyi kabul eden desen → 516;
> 47 atlanmış alt test görünmüyordu. Desen `Makefile`'ın yazdığıyla birebir
> aynıdır (satır 169 ve kapının kullanıcıya bastığı satır 208) — iki yerde iki
> farklı desen, bayat bir sayıdan beterdir.
>
> ⚠️ **F7'nin metni bu ajandan çıkarılamaz** ve uydurulmayacak: yukarıdaki eşleme
> bir **kanıt**, bir **tahmin listesi değil**. F7'nin ne olduğunu ve akıbetini
> yazacak olan, FAZ A'nın raporunu elinde tutan **orkestratördür**; bu blok o
> boşluğun yerini ve büyüklüğünü kaydediyor, doldurmuyor.
>
> 🔴 **T40'IN ÜÇ SORUSUNDAN İKİSİ HÂLÂ AÇIK — ÖLÇÜLDÜ, KARARA BAĞLANMADI**
> (karar orkestratörün; bu blok yalnızca iki okumayı ölçüyor).
>
> **(ii) `/0`'dan dar ama hâlâ geniş bir aralık: reddedilsin mi, yoksa uyarılıp
> `trust`'a mı yansısın?** Ölçüm (gelişim DB'si, 2026-08-19 21:40 UTC — mekânlar
> taşıdıkları **en geniş** aralığa göre): `/0` → **3** · `/1` → **1** · `/24` →
> **199 548** · `/29` → **1** · `/32` → **9**. İlk dördü bu kartın kendi mutasyon
> kalıntısı; onları çıkarınca `/24`'ten geniş bir aralık taşıyan **gerçek mekân
> yok**, yani *"/16'dan geniş reddedilsin"* bugün **sıfır** yanlış pozitif verirdi.
> ⚠️ Ama 199 548 satır **aynı tohumlanmış `/24`**, yani bu veri gerçek bir kurulumun
> ne sakladığı hakkında **zayıf kanıt**. **`trust`'a yansıtmanın maliyeti ise
> zayıf değil, yapısal:** `CLAUDE.md` §5 güven puanını *"20 (taban) + 50 (IP
> eşleşti) + 30 (GPS eşleşti)"* diye **normatif** olarak sabitliyor ve
> `trustScore` iki boolean'ın saf fonksiyonu. Dereceli bir IP ağırlığı §5'i
> **değiştirmek** demektir — bir kod değişikliği değil, bir **§5 tadili + ADR**.
>
> **(iii) `ip_match` notu aralık genişliğini taşımalı mı?** Ölçüm: `Decision.Note`
> eşleşen politika ifadesinin `reason`'ından **birebir** geliyor
> (`internal/domain/tap/decide.go`), ve o metin tenant başına `policy_versions`'ta
> **donmuş** (append-only; 48 763 tenant version 1'i tutuyor). Yani **belgeyi**
> değiştirmek M7-03'ün akışını ister. **Ucuz yol var ve zaten mevcut:** M4-04'ün
> `appendNote` mekanizması notun sonuna hesaplanmış bir ek yazıyor
> (`stale open check-in` böyle basılıyor) — genişlik eki ~5 satır + test, migration
> yok, politika belgesi değişmiyor. Bedeli: değişmez bir satıra bir sayı daha
> girer (§4.7 açısından sorun değil, önek uzunluğu sır değil).

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B2 4. turu sırasında).**
>
> 🔴 **BU TURUN İKİ BLOKLAYANI DA "KAPI SANILAN AMA KAPI OLMAYAN" SINIFINDANDI.**
>
> **(1) `NO FORCE ROW LEVEL SECURITY`'nin hiçbir kapısı yoktu, ama script kapı
> gösteriyordu.** Güvenlik denetimi mutasyonu sevk edilmiş şemaya uyguladı: sahibin
> bağlantısı **yabancı** bir `app.tenant_id` altında **404 335 satır / 80 961 tenant**
> okudu ve `internal/db` suiti **tamamen yeşil** kaldı (8.802 s). Sebep yapısal:
> izolasyon suiti `tappa_app` ile koşar, `tappa_app` **hiçbir tablonun sahibi
> değildir** (ADR 0002 md. 1 bunu şart koşar) ve `FORCE`'un tek işlevi politikaları
> tablonun **sahibine** de uygulamaktır. Üstelik `relforcerowsecurity` iddiası yalnız
> **5** tabloda vardı; **17** tenant kapsamlı tablonun **11'inde** — `transactions`
> dahil — hiç yoktu. Kapatıldı: `internal/db/rlsforce_test.go`, listeyi
> `db/migrations`'tan **türetir** (ad listesi yok, taban 17), her tablo için
> `relrowsecurity` **ve** `relforcerowsecurity` okur. Pozitif kontrol iki yönlü ve
> ölçüldü — `ALTER TABLE password_resets NO FORCE …` ve
> `ALTER TABLE employee_invites DISABLE …` sevk edilmiş şemaya uygulanıp geri alındı;
> ikisinde de kapı **kırmızıya döndü ve tabloyu adıyla söyledi**.
>
> **(2) "Tam kapsama" yüklemi yanlış uzayda soruluyordu.** Panele sekiz CIDR
> yapıştırıldı (`11.0.0.0/8 · 8.0.0.0/7 · 12.0.0.0/6 · 0.0.0.0/5 · 16.0.0.0/4 ·
> 32.0.0.0/3 · 64.0.0.0/2 · 128.0.0.0/1`; sınır 32, yani **dörtte biri**), yazma
> tarafı **kabul etti** ve `203.0.113.7`'den gelen tap **NFC ve QR'da** `verdict=ok ·
> ip_match=true · trust=70` verdi — QR satırı 3. turun kapattığı çıktının birebir
> aynısı, yani `base:qr-requires-ip` yine kapalı. Dışarıda kalan tek blok
> `10.0.0.0/8`'di: liste **hiç kimseyi** elemiyordu. Yüklem **istemci uzayına**
> taşındı (`netx.CoversEveryClientAddress`; ad da değişti, çünkü eski ad bu girdi
> için artık yanlış olurdu). **Yanlış pozitif ölçümü: 323 914 mekânın 19 farklı
> listesi, eskiden reddedilen 30 · bugün reddedilen 30 · yeni 0.** Sayılmış bedeli
> ADR 0005'te (public uzay ≠ tamamen özel kurulum).
>
> **Ayrıca bu turda:** `scripts/redline-check.sh`'e **tek bir normalizasyon adımı**
> (noktalama etrafını boşluklandırma) — dokuz sözlüksel kaçış **birden** kapandı ve
> desen sayısı **artmadı**; sınıf→kapı haritasının **her satırı** birer mutasyonla
> ölçüldü (RLS DISABLE · DROP POLICY · trigger sökme · GRANT ALL · rol üyeliği ·
> `OWNER TO` → hepsi kırmızıya döndü, hepsi geri alındı); `R5b`'nin ne olduğu
> dürüstçe yazıldı (**uyarı sistemi**, kapı değil); `viewsecurity_test.go` kendi
> sınırını yazdı (`SECURITY DEFINER` + `RETURNS TABLE(...)` üzerinden view **katalog
> testinde de görünmüyor** — doğrudan 0 satır, view'den 408 188 satır / 81 777
> tenant); ve W4 muafiyetinin kalanı **dördüncü kez daraltılmak yerine sayıldı**
> (markdown kod işareti ile Go ham dize sınırlayıcısı **aynı karakterdir**; ayırt
> edebilecek tek şey bir ayrıştırıcıdır).

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B2 5. turu sırasında).**
>
> 🔴 **AYNI KUSUR SINIFININ ÜÇÜNCÜ TEKRARI: BİR UZAYI İSİM SAYARAK TANIMLAMAK.**
> 4. turun *"istemci uzayı"* onarımı yüklemi **13 adlandırılmış bloğa** bağlamıştı;
> listede olmayan her artık blok bir kaçış üretiyordu. İki yeni yazım ölçüldü,
> ikisi de **aynı boyda, aynı biçimde**:
>
> | yazım | 4. tur yüklemi | kapsanan adres |
> |---|---|---|
> | `10.0.0.0/8` tümleyeni (8 satır) | reddediyor | 4 278 190 080 |
> | `25.0.0.0/8` tümleyeni (8 satır, **tek hane farkla**) | **kabul ediyor** | 4 278 190 080 |
> | `192.0.2.0/24` tümleyeni (24 satır) | **kabul ediyor** | 4 294 967 040 |
>
> Sonuncusu **hiç kimseyi elemiyor**: `192.0.2.0/24` RFC 5737 TEST-NET-1'dir, hiç
> yönlendirilmez. Üçü de `verdict=ok · ip_match=true · trust=70` üretiyordu,
> **QR dahil** — yani `base:qr-requires-ip` üçüncü kez devre dışıydı.
>
> **Karar: ad listesi silindi, yüklem BÜYÜKLÜĞE çevrildi**
> (`netx.TooWideForProofOfPlace` — üçüncü ad, ve ilk kez davranışı anlatıyor). Bir
> liste, aile başına **bir ISP tahsisinin iki katından** fazlasını kapsıyorsa yer
> kanıtı değildir: IPv4'te bir `/7` (2^25; birim bir `/8`), IPv6'da bir `/31`
> (2^97; birim bir `/32` = RIR'ın LIR'a verdiği asgari tahsis — `/8` v6'ya prefix
> uzunluğuyla da oranla da taşınmaz, o yüzden **anlamından** yeniden türetildi).
> İkiye katlama ölçümden geliyor: tamamen özel bir kurulum (`10/8` + `192.168/16`)
> çıplak bir `/8`'i **65 536 adresle** aşıyor.
>
> **Yanlış pozitif ölçümü, tüm gerçek mekânlara karşı:** 332 855 mekân, 214 115'i
> aralıklı, **19 farklı liste**. En geniş **gerçek** liste 264 adres
> (`{81.240.16.8/29, 192.168.1.0/24}`, 44 mekân) — limit onun **127 100 katı**.
> Reddedilen **4 liste / 48 mekân**, 48'inin **48'i** bu kartın kendi kalıntısı
> (adları söylüyor: *universal* 33, *St Julians* 11, *Everywhere* 4);
> **gerçek mekân reddi 0**. ⚠️ Sayı her koşuda büyüyor (aynı tur içinde
> 331 499 → 332 855), **küme** büyümüyor.
> Eşik iki uçtan **teste bağlandı** (`TestProofWidthLimitsSitBetweenTheMeasuredExtremes`):
> meşru tavanın altına inerse de, sömürü tabanının üstüne çıkarsa da kırmızı.
>
> **(2) `ADD COLUMN tenant_id` yönünü hiçbir kapı görmüyordu.** `rlsforce_test.go`
> listeyi yalnız `CREATE TABLE` gövdesinden türetiyordu; ölçüldü:
> `CREATE TABLE zz_late_scoped (id uuid); ALTER TABLE … ADD COLUMN tenant_id …` →
> türetme 17'de kaldı, tablo hiçbir listede yok, dosya **yeşil**. Kapatıldı: kapının
> listesi artık **canlı katalogdan** geliyor (`pg_attribute`'ta `tenant_id` taşıyan
> tablolar), migration türetmesi **boşluk-karşıtı çapraz kontrol** olarak duruyor ve
> ikisi ayrışırsa test **yönü adıyla** kırmızıya dönüyor. Yeni pozitif kontrol:
> `TestRLS_TheGateSeesATableScopedAfterItWasCreated` (BEGIN … ROLLBACK içinde).
>
> **(3) `BaselineVersion`'ın ne işaretlediği** `baseline.go`'da gerçeğe eşitlendi:
> damga aynı tarihle artık iki farklı `Reason` metnini işaretliyor, hiçbir kapı
> bunu görmüyor (`TestBaseline_VersionStamp` yalnız damganın **tutarlılığını**
> sınıyor), ve **bilerek yükseltilmedi** — yükseltme bir *duyurudur*, M7-03'ün
> kabul akışı yok. Geçmiş kayıtlar `policy_versions.document` sayesinde kendi
> metinleriyle açıklanıyor (§4.3/§4.6 ihlali yok).
>
> **(4) Ucuz kapanışlar.** `Makefile`'ın SKIP komutu **alt testleri saymıyordu**
> (`^--- SKIP` → 469, girintiyi kabul eden desen → **516**; 47 atlanmış alt test
> görünmüyordu); harf çevirisi tamamlandı (`calisma-anı` → `calisma-ani`);
> `rlsforce_test.go`'nun başlığı artık iki yönü de (`ADD`/`DROP COLUMN`) sayıyor.
> ⚠️ `billing_periods`'ta `employee_count=999` taşıyan **1 kalıntı satır** var
> (tenant `bad47dbe-…`, `iso-fixture`, sentetik) — **silinmedi**, satır sayısı
> azaltılmaz; tarihli bir gözlem olarak yazıldı.

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B3 — SON FAZ).**
>
> **FAZ B3 kartın SAĞLANMAYAN TEK KRİTERİNİ hedefledi:** *"ORTA/DÜŞÜK olanlar ya
> kapandı ya gerekçesiyle kabul edildi **ve yazıldı**."* B1 ve B2 kapatma kısmını
> yaptı; bu faz **"ve yazıldı"** kısmıdır.
>
> **KABUL METİNLERİ ARTIK AĞAÇTA: [ADR 0005](../adr/0005-kabul-edilen-riskler.md)
> → *"Kabul edilen ORTA/DÜŞÜK denetim bulguları (M8-04 FAZ B3)"*.** Her satır
> *ne kabul edildi · neden kapatılmadı · mekanik çapa* taşır; **satır sayısı burada
> YAZILI DEĞİL, ADR'de BAĞLI** (`TestADR0005_TheAnchorCountsMatchTheProse`).
> 🔴 **Çapaların ÇOĞU bir TEST
> ADIDIR ve bu bilinçli:** `TestEveryNamedTestExists` deponun **her metin dosyasını**
> tarar, ADR dahil — yani adı geçen bir test silinirse belge kırmızıya döner. Pozitif
> kontrol koşuldu (bir çapa testi yeniden adlandırıldı → `docs/adr/0005-…` adıyla
> raporlandı, `54 live / 53 budgeted`; geri alındı). **Backlog'a yazmak yetmiyordu:**
> `docs/backlog.md` kendi başlığında *"yalnız KULLANICININ yapabileceği işleri
> tutar"* diyor; oraya yazılan bir **karar** karar değil hatırlatma olur.
>
> 🔴 **ÜÇ SATIRIN MEKANİK ÇAPASI YOK VE BU AĞACA YAZILDI: T28 · T38 · T39.** Üçü de
> **tarayıcı ya da küme** davranışına bağlı; bir Go testi `Sec-Fetch-Site` üretmez,
> **taklit eder**, ve taklit eden bir test bir kabulü doğrulamaz. Gerçek kapıları
> dağıtımdır (TLS + ingress). *"Kapatıldı"* demek bu görevin dört turdur tekrarlayan
> **sağlanmayan-garanti** kusurunu bir kez daha işlemek olurdu.
>
> **BU TURDA KAPATILAN ALTI BULGU — hepsi mutasyon + pozitif kontrolle:**
> **T48-1** `TestRotateScript_AlwaysPassesOnErrorStop` `HasPrefix("psql ")` ile
> tarıyordu ve `BYPASS="$(psql …` satırını **görmüyordu** — script'in **dört**
> çağrısının **üçü**; oysa `deploy/README.md` bunu *"her `psql` çağrısında"* diye
> yayımlıyor. Kardeş testin (`TestRotateScript_IsImmuneToAHostilePsqlrc`) **kanıtlanmış
> kuralı** kopyalandı, körlük tabanı 3→**4** · **T48-2** `lock_timeout` **değeri**
> pinsizdi (`'5s'` → `'5000s'` **yeşil**, ölçüldü) ve runbook onu **iki kopyada**
> yayımlıyor; ikisi de bağlandı · **T54-1** olay adları **alt-dize** ile aranıyordu:
> `deploy/README.md`'de `tap.decision` → `tap.decisionMUT` (yapıştırılabilir blok
> **dahil**, 6 yer) **GEÇİYORDU** — operatörün kopyaladığı filtre hiçbir şeyle
> eşleşmeyecekken. Tam-ad eşleşmesine çevrildi · **T55** iki taşıyıcı script'in
> arkasında **hiçbir kapı yoktu** ve biri **veri siliyor**: `cmd/tappa/scriptguards_test.go`
> (beş test) — pin literalleri, sıra (`cmp` **önce**, `down --volumes` **sonra**),
> §5'in dört tetikleyici yüklemi, ve **davranış**: `DOCKER_HOST=tcp://…`/`ssh://…`
> ile script gerçekten koşturulup **reddi** ölçülüyor (yıkıcı dal **hiç**
> çalıştırılmıyor) · **T56-2** `db-reset.sh` operatöre elle yazılmış
> `docker compose -p tappa logs db` diyordu — proje adı **ikinci kez** yazılmış, `-f`
> **yok**; dosyanın kendi teşhisi, bir ekran ilerde. Artık `${COMPOSE[*]}`'den
> basılıyor · **T56-1** yerellik sondasının **reddettiği hedefte yazma yaptığı**
> (`probeleak_default` network + `probeleak_probeleak-pgdata` volume) script başlığına
> **sayılmış limit** olarak yazıldı.
>
> **VE SON DENETİMİN İKİ İŞİ:**
>
> **(1) Fişin `note`'u kararın hangi belgeden geldiğini söylemiyordu — KAPATILDI
> (savunma derinliği).** Ölçüldü: `docket.templ` `d.Note`'u **katmansız** basıyordu,
> `policy_layer` satırda dururken. Kapatıldı **okuma tarafında** (yazma tarafı değil —
> bir yazma işareti **var olan** satırlara ulaşmaz, ve §9'un kutsal saydığı çalışan
> ekranını da değiştirirdi): `policy_layer` iki sorguya eklendi (gün **ve** onay
> kuyruğu), `ledger.Record.NoteIsTenants` türetiliyor, fiş **kararı tenant'ın kendi
> belgesinin verdiğini** söylüyor. Pozitif kontrol iki yönlü (`if true` → etiket her
> fişte → kırmızı; `if false` → hiç → kırmızı).
>
> 🔴 **BU MADDENİN İLK İKİ YAZIMI ETİKETİ *"tenant'ın YAZDIĞI sahte kanıt cümlesi"*
> DİYE TANITIYORDU VE BU YANLIŞTI — DÜZELTME TURU 2'DE ÖLÇÜLDÜ.** Tenant bir ifadeyi
> **seçip kapsamlandırabiliyor**, **metnini yazamıyor**: `copyOfShipped` ifadeyi
> **bütün** kopyalıyor (effect · action · condition · **reason**), yalnız `Sid` ve
> `Resource` değişiyor; `AuthorCommand`'da `Reason` alanı **yok**; panel formu
> `op · policy · resource · name · based_on · venue` okuyor, **doküman yüklemiyor**.
> Etiketin kendisi doğru — *"bu kararı tenant'ın kuralı verdi"* diyor — ama onu tarif
> eden yorumlar üç dosyada yanlıştı ve düzeltildi. Kapılar artık **davranışsal**:
> `TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs` (üretici çağrılıyor,
> ifadeler alan alan karşılaştırılıyor) ve
> `TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument` (belgeyi yazan
> **iki** üretim çağrısı; üçüncüsü — Q22'nin M9-07'ye ertelediği ham-JSON editörü —
> kırmızı verir). Metin tarayan kapı **silindi**: bugün **doğru** olan bir cümleyi
> yasaklıyordu.
>
> **(2) `CREATE RULE … DO INSTEAD NOTHING` kapısı — ÖLÇÜLDÜ, HARİTA DOĞRU ÇIKTI.**
> Bu, sınıf→kapı haritasının **ölçümü tekrarlanmayan tek satırıydı** ve aynı haritanın
> başka iki satırı yanlış çıkmıştı. Sonda commit'li DDL gerektirdi (bir RULE'un etkisi
> başka bir bağlantıya görünür olmak zorunda): **Row1·2·4·5·6·7 FAIL, Row3 PASS** —
> yani **7'nin 6'sı**, ve PASS eden tam olarak haritanın *"dönmemeli"* dediği
> `TestCheckinDB_Row3_NoSessionRedirectsAndWritesNOTHING`. Geri alındı ve doğrulandı:
> `pg_rules = 0`, `transactions` **426 791 → 426 791**. **Sayı doğru; düzeltilecek bir
> şey yok, değişen tek şey artık iki kez ölçülmüş olması** (tarih script'e yazıldı).

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B3 doğrulama turu).**
>
> 🔴 **FAZ B3'ÜN İŞİ AĞAÇTAYDI VE BU TUR ONU DOĞRULAMAK İÇİN AÇILDI; İKİ İDDİA
> YANLIŞ ÇIKTI — İKİSİ DE *"AĞ TUTUYOR AMA AĞ HAKKINDAKİ CÜMLE YANLIŞ"* SINIFINDAN.**
> Ürün davranışında bulgu **yok**; yanlış olan **belgenin kendisi hakkındaki**
> cümleleriydi, yani tam olarak bu görevde **yedi kez** bloklayan sınıf.
>
> **(1) ADR 0005 kendini otuz satır arayla çürütüyordu.** Bölüm başlığı *"**HER SATIR**
> BİR MEKANİK ÇAPA TAŞIR, VE ÇAPA BİR TEST ADIDIR"* diyordu; aynı belge aşağıda
> *"**ÜÇ SATIRIN** mekanik çapası yok"* diyordu (T28 · T38 · T39). İkisi aynı anda
> doğru olamaz. Başlık **gerçeğe** eşitlendi.
>
> **(2) Bu kart tabloyu *"On bir satır"* diye tanıtıyordu; tablo **on iki** satır.**
> Sayıyı hiçbir kapı korumuyordu. **Düzeltilmedi — kaldırıldı ve BAĞLANDI:** karttan
> silindi, ADR'de `TestADR0005_TheAnchorCountsMatchTheProse` ile bağlandı.
>
> **(3) Ve sayılamıyordu, çünkü damga İKİ TÜRLÜ yazılmıştı.** T38/T39
> *"MEKANİK ÇAPASI YOK, SAYILDI"*, T28 *"bu satırın MEKANİK bir çapası YOKTUR ve bu
> sayılmıştır"* diyordu — aynı anlam, ve ilk yazımın sayımı **2** verirken düzyazı
> **3** diyordu. Bir kümeyi **insanların onu yazma biçimlerini sayarak** tanımlamak,
> `netx`'in üç turda üç kez aşılan kusuruyla **aynı** kusurdur; onarım da aynı şekilde:
> **tek kanonik damga**.
>
> **Yeni kapı — dört iddiayı bağlıyor** (`cmd/tappa/adr0005_test.go`): satır sayısı ·
> çapasız satır sayısı · çapasızların **adları** (yalnız sayı, bir satırın çapasını
> yitirip başkasının kazanmasını görmez) · ve **damgasız her satırın çapa hücresinin
> gerçekten bir `Test…` adı andığı**. **Dördüncüsü `TestEveryNamedTestExists`'in
> GÖREMEDİĞİ boşluktur** ve ölçüldü: T52'nin çapa hücresi `--` yapıldığında **benim
> kapım FAIL**, `TestEveryNamedTestExists` **aynı ağaçta `ok`** — çünkü o *"anılan ad
> var mı"* diye sorar, *"anması gereken satır anıyor mu"* diye değil.
> Üç mutasyon + pozitif kontrol koşuldu, üçü de geri alındı (`cmp` **OK**).
>
> ⚠️ **Doğrulanan ve DEĞİŞTİRİLMEYENLER:** orkestratörün sekiz satırlık ön ölçümünün
> **sekizi de tuttu**; ADR'de adı geçen **her çapa testi gerçekten var** — ve buraya
> bir sayı yazılmıyor, çünkü onu tutan bir kapı zaten var (`TestEveryNamedTestExists`);
> ⚠️ **bu cümlenin ilk hâli *"25 çapa testinin 25'i"* diyordu ve sayı ölçülmemişti
> (gerçek değer 20) — yapıcının bu turda kendi işlediği, düzelttiği ve kaydettiği
> kusur, kuralın yazara da uygulandığının kanıtı olarak burada bırakıldı.**
> `TestEveryNamedTestExists`'in *"her metin dosyasını tarar"* iddiası **doğru**
> (`filepath.Walk(repoRoot)` + dört girdilik atlama listesi), yani ADR gerçekten
> taranıyor ve B3'ün çapa mantığı **sağlam**.

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B3 — düzeltme turu).**
>
> Doğrulama turu **RED** verdi (iki bloklayan, iki bloklamayan). Dördü de bu turda
> kapatıldı. 🔴 **Ve bloklayanların ikisi de yine aynı sınıftandı:** ürün doğru
> davranıyordu, **ürün hakkındaki cümle** yanlıştı ya da hiç yazılmamıştı.
>
> **(D42 — SONRADAN GERİ ÇEVRİLDİ, ↓ düzeltme turu 2) ÜÇÜNCÜ KOPYA `view.go`'DAYDI.**
> Bu tur `web/templates/pages/view.go`'daki *"internal/policy's reasons are fixed
> strings written by us"* cümlesini **yalan** sayıp sildi, yerine tenant'ın
> *"author-written `reason`"*'ının `Note`'a birebir düştüğünü yazdı ve bunu bir metin
> tarayıcısıyla **dayattı** (`cmd/tappa/noteprovenance_test.go`'nun ilk hâli; adı
> burada anılmıyor, çünkü artık var olmayan bir teste yapılan atıf **çalışan bir
> atıf gibi görünür** ve `TestEveryNamedTestExists` de haklı olarak kırmızı verir).
> 🔴 **BİR SONRAKİ DENETİM BU İDDİAYI ÖLÇTÜ VE ÇÜRÜTTÜ:** silinen cümle
> **doğruydu**. Bu blok, düzeltmenin kendisi kadar önemli olduğu için silinmiyor —
> ayrıntı ve bugünkü kapılar için aşağıdaki *"düzeltme turu 2"* bloğuna bakın.
>
> **(D42-3 — SONRADAN GERİ ÇEVRİLDİ, ↓ düzeltme turu 2) TAP EKRANI KATMAN ETİKETİ
> TAŞIMIYOR.** Bu tur bunu *"sayılmış limit"* olarak yazdı ve metnini bir kapıyla
> (aynı silinen dosyadaki ikinci test) tuttu. Dayanağı D42'nin
> çürütülen iddiasıydı: tenant çalışanının ekranına **kendi yazdığı** bir cümleyi
> düşürebiliyor olsaydı, etiketsiz ekran gerçekten bir dürüstlük boşluğu olurdu.
> Cümle her katmanda **bizim** olduğu için o boşluk yok; ekranda eksik olan tek bilgi
> *"kararı hangi belge verdi"* ve onun muhatabı **müdür** (fişte basılıyor), çalışan
> değil. Kapı da metin de kaldırıldı. §9 gereği ekrana **hâlâ dokunulmadı**.
>
> **(D43) KABUL KRİTERİ 4 — DÖRT MADDENİN ÜÇÜ ÖLÇÜLDÜ VE YAZILDI, BİRİ BLOKE.**
> Bu satır *"Manuel doğrulamalar"* diyor ve bugüne kadar yalnız birinci maddesi
> yazılıydı. 🔴 **Aşağıdakiler bu turda YENİDEN ölçüldü** (2026-08-20, `HEAD ac29cf7`
> + bu turun çalışma ağacı, yerel `tappa` DB'si); *"sağlanıyor"* denmedi, komutu ve
> çıktısı yazıldı.
>
> **1. Çapraz-tenant erişim denemesi — ÖLÇÜLDÜ, GEÇTİ.** Üç ayrı kesit:
> · **Tap yüzeyi:** `TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING` **PASS**
> — başka tenant'ın plaketine geçerli bir oturumla POST → **403**, **her iki**
> tenant'ın `transactions` sayısı **değişmiyor**, ve testin kendi **pozitif kontrolü**
> aynı istek şeklinin kendi plaketinde **+1 satır** yazdığını gösteriyor (yani ölçülen
> şey tenant kontrolü, alakasız bir ret değil).
> · **Sayaç:** `TestCheckinDB_ForeignTenantTapNeverTouchesTheOtherTenantsCounter`
> **PASS** — yabancı plaketin `last_ctr`'ı **kıpırdamıyor** (900 → 900), pozitif
> kontrolde o tenant'ın **kendi** oturumu aynı plaketi 900 → 901 ilerletiyor. Kod
> tarafındaki sebep tek satır: `internal/domain/checkin/checkin.go:954`
> — `if tagRow.TenantID != req.SessionTenantID { return false, 0, nil }`, yani
> `sun.AdvanceCounter` **hiç çağrılmıyor**.
> · **Panel girişi:** `TestAuthenticate_RefusesTheCrossTenantBypass` **PASS** (üç alt
> test; *"aynı parola iki işletmede"* vakası dahil).
> · **Veri katmanı:** `TestRLS_ReadIsolation_AllTables` **PASS** — **17 alt test**,
> ve bu sayı **elle yazılmadı**: 16'sı canlı katalogda `tenant_id` taşıyan tablolar
> (`pg_attribute` sorgusu aynı ağaçta **16** döndürdü), 17.'si `tenants`'ın kendisi
> (`id` ile kapsanır). Yanında `TestRLS_WriteWithCheck_AllTables` ·
> `TestRLS_NoContext_FailsClosed` · `TestRLS_AppRoleHasNoBypass` — dördü de **PASS**.
>
> **2. Oturum çalma senaryosu — ÖLÇÜLDÜ. Kapatılmayan kısmı aşağıda, gizlenmedi.**
> **Çalınmış bir tap çerezi NE YAPAMAZ:** · Tek başına **hiçbir kayıt üretemez**:
> `POST /api/checkin` gövdesinde **bu sunucunun mühürlediği** bir `ctx` ister ve o
> mühür **oturum kimliğine bağlıdır** (`t.contexts.parse(…, id.Session.ID)`,
> `internal/handler/checkin.go:176`). O bağlamı üreten `GET /t`, geçerli bir SUN
> URL'i ister → **fiziksel dokunuş** (§5 satır 2). · **Panele erişemez:** ayrı çerez,
> ayrı tablo (`admin_sessions`), ve yol `/admin`'e daraltılmış —
> `TestPanelCookies_NeverReachTheTapSurface` + `TestPanelCookiePath_IsNarrowerThanTheTapSurface`.
> · **Başka tenant'a geçemez:** oturum kendi `tenant_id`'sini taşır, plaket uyuşmazlığı
> `sys:tenant-mismatch` (yukarıdaki ölçüm). · **Çerezin kendisi kayıt değildir:** DB
> yalnız HMAC-SHA256 **hash**'i tutar, Go tarafında bayt karşılaştırması **yok**
> (`internal/session/manager.go`).
> **NE YAPABİLİR:** hırsız, kurbanın tenant'ının bir plaketine **fiziksel olarak
> dokunabiliyorsa**, kurban adına giriş/çıkış yazdırabilir. Bu, ADR 0005'in Risk 1'i
> (buddy punching) ile **aynı** kalıntıdır ve orada kabul edilmiştir.
> 🔴 **KAPATILMAYAN, DÜRÜSTÇE:** **(a)** Oturum **FIXATION**'ı — `SameSite`/`HttpOnly`/
> `Secure` bunu durdurmaz; T3'ün kabul metni bunu zaten söylüyor
> (`internal/handler/cookies.go`) ve bu turda **ADR 0005'in tablosuna** satır olarak
> eklendi. **(b)** 🔴 **ÇALINMIŞ BİR TAP OTURUMUNU İPTAL EDECEK BİR EKRAN YOK — ÖLÇÜLDÜ.**
> `session.Manager.Revoke` ve `ListForEmployee`'nin **üretimde tek bir çağıranı bile
> yok**; `RevokeAllForEmployee`'nin tek çağıranı `internal/handler/activate.go:458`
> (yeniden aktivasyon eskilerin **hepsini** öldürür). `admins.Revoke` yalnız **panel**
> oturumu içindir (`adminlogin.go:1336`). Yani bugün tek çare **çalışanı yeniden
> aktive etmek**tir; çalışanı `deactivated` yapmak oturumu **bilerek** iptal etmez
> (`internal/handler/tap.go:304-309`) ama sonraki her tap `sys:employee-deactivated`
> ile **kayıtlı bir reject** + güvenlik uyarısı üretir (§5 satır 4). Bunu üreten komut:
> ```sh
> grep -rn "RevokeAllForEmployee\|ListForEmployee\|\.Revoke(" --include='*.go' internal/ cmd/ | grep -v _test
> ```
> **(c)** Oturum başına oran sınırı floodu durdurur, **tek bir sahte tap'i durdurmaz**
> (aşağı bakın). Bir *"cihazlarım / oturumu kapat"* ekranı **bu görevin işi değildir**;
> burada **sayılmış boşluk** olarak yazıldı, kapatılmadı.
>
> **3. Oran sınırı — ÖLÇÜLDÜ.** **Anahtar hiçbir yerde tenant değildir**; üç şekil
> var: **adres** (`httpx.ClientIP`, IPv6'da `/64`), **oturum**, ve **hesap**.
> ⚠️ **Aşağıdaki bütçeler TARİHLİDİR** (2026-08-20, aynı ağaç) ve hepsi tek komutla
> yeniden üretilir:
> ```sh
> grep -rn "Limit  *= \|Period  *= " internal/httpx/ratelimit.go internal/handler/ratelimit.go internal/handler/adminratelimit.go internal/handler/signupratelimit.go
> ```
> · **Tap yüzeyi** (`GET /t`, `POST /api/checkin`) **iki taraflı**: adres başına
> **3000/10 dk**, oturum başına **300/10 dk** (`internal/httpx/ratelimit.go:284-287`).
> Bu iki sayı **bağlı**: `TestTapLimiter_DefaultsAreWideEnoughForAShiftChange` —
> daha önceki bir taslak 120 yazmıştı ve o test 120'nin tam olarak beş saniyelik bir
> yeniden yükleme döngüsünün ürettiği sayı olduğunu yakaladı.
> · **Aktivasyon:** adres başına **600/10 dk** flood, **60/10 dk** atfedilemeyen
> başarısızlık, **10/10 dk** davet hatası (`internal/handler/ratelimit.go:133-147`).
> · **Panel:** adres **3000/10 dk**, oturum **300/10 dk**, giriş denemesi **10/10 dk**,
> bcrypt işi **120/10 dk**, hesap başına **10/10 dk** (`adminratelimit.go`).
> · **Kayıt (signup):** adres **600/10 dk** flood, VIES **20/10 dk**, deneme
> **10/10 dk**, **işletme yaratma 3/saat** (`signupratelimit.go:62-146`).
> 🔴 **YAPISAL KALINTILAR — KAPATILMADI, `docs/backlog.md` T1:** sınırlayıcı
> **süreç içi** (iki instance sınırı **ikiye katlar**), **sabit pencere** (pencere
> sınırında kısa sürede **2×**), ve `limiterMaxKeys` (**100 000**) aşılınca map
> **toptan sıfırlanıyor** — bilinçli **fail-open**, `internal/httpx/ratelimit.go:24-32`
> ve `:142-151`'de yazılı, `TestLimiter_EvictsWhenTheMapIsFull` ile bağlı.
> ⚠️ **`/healthz` ve `/readyz`'in adres başına bütçesi YOK** ve bu bir eksiklik değil
> **karar**: canlılık, her bağımlılık çökmüşken bile cevaplanabilmek zorunda
> (`internal/httpx/router.go:85-109`). `/readyz` **bir DB sorgusu** koşturuyor, yani
> M8-01'in notu duruyor: bütçesiz bir uç, ucuz olmayan tek sağlık ucudur.
>
> **4. Replay denemesi GERÇEK ETİKETLE — HÂLÂ BLOKE, ÖLÇEMEDİM.** Sebep değişmedi:
> NTAG 424 DNA yazıcı donanımı **elimde yok**, bu yüzden gerçek bir çipin ürettiği
> SUN URL'i üretilemiyor. Yerine geçen **değil**, tamamlayıcı olan şey ağaçta: bilinen
> cevap vektörleri (`internal/sun`) ve `TestAdvanceCounter_ConcurrentRaceExactlyOneWinner`
> (N goroutine, aynı `(tag, ctr)`, tam olarak **1** kazanan, `-race`). Gerçek etiketli
> tur **M8-05 FAZ B**'ye bağlıdır. 🔴 *"Sağlanıyor"* denmiyor — **ölçülemedi**.
>
> **(D44) ADR 0005'İN KÜME CÜMLESİ TABLOSUNDAN GENİŞTİ — İKİ ONARIM DA YAPILDI,
> ÖLÇEREK.** Denetim *"(a) T3'ü ekle **ya da** (b) cümleyi daralt"* diyordu; ölçüm
> **ikisini de** gerektirdi ve gerekçesi şu: **(a)** T3 gerçekten bir **kabul**dür
> (`state.md` FAZ B2'yi *"T27/T3 kabul, ölçülerek"* diye kaydediyor ve üç ölçüm
> `internal/handler/cookies.go`'da duruyor) → satır **eklendi**, çapası
> `TestPanelCookiePath_IsNarrowerThanTheTapSurface` + `TestCookies_ZeroValueIsSecure`,
> ve bağlı sayı **12 → 13** oldu (kapı zaten tutuyordu, `çapasız satır = 3`
> değişmedi). **(b)** Ama T3 eklendikten **sonra da** tablo *"kapatılmayanların
> tamamı"* değildi: **T40'ın (ii)/(iii) soruları** ölçüldü ama **karara bağlanmadı**,
> ve FAZ A'nın **F7**'sinin metni repoda **yok**. İkisi de **kabul değil**; tabloya
> girselerdi *"kabul edildi"* diye okunurlardı. Bu yüzden cümle
> *"kapatılmayıp **KABUL EDİLEN**"* diye daraltıldı ve dışarıda kalanlar **adıyla**
> sayıldı. (⚠️ O listenin dördüncü maddesi D42-3'tü; düzeltme turu 2'de geri çekildi,
> çünkü dayandığı iddia çürütüldü — ADR'de de kaydı var.)
>
> **(D45) KAPININ REGEX'İ DÜZYAZI SIRASINA BAĞLIYDI — DÜZELTİLDİ VE MUTASYONLA
> KANITLANDI.** `satır = (\d+)`, ADR'nin `**satır = 13** · **çapasız satır = 3**`
> cümlesinde **ikincisinin içine de** uyuyor; doğru okuması yalnız **leftmost-match**
> sayesindeydi. Mutasyon (iki ifade yer değiştirildi, ADR'nin kopyası üzerinde):
> eski desen *"holds 3 rows"* dedi, **yeni desen 13 okumaya devam etti**. İkisi de
> `\*\*…\*\*` ile çapalandı.

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B3 — DÜZELTME TURU 2).**
>
> Güvenlik denetimi **RED** verdi ve bulgusu **bir önceki turu tersine çevirdi**.
> 🔴 **BU TURUN DERSİ TEK CÜMLE: BİR YANLIŞI DÜZELTİRKEN YERİNE KONAN CÜMLE DE
> ÖLÇÜLMELİ.** Önceki tur *"tenant'ın yazdığı `reason` çalışanın ekranına birebir
> düşüyor"* iddiasını doğru sayıp üç dosyadaki **doğru** cümleyi sildi, yanlışını
> koydu ve doğru cümleyi bir kapıyla **yasakladı**.
>
> **(D46 — BLOKLAYAN) SİLİNEN CÜMLE DOĞRUYDU. ÖLÇÜM (kendi komutlarımla yeniden
> üretildi; ⚠️ satır numaraları bu ağaca aittir — kayarlar, **dosya adları ve
> davranış** kaymaz, ve davranışı tutan kapılar aşağıda adıyla anılıyor):**
> · `policy_versions.document`'ın **iki** üretim yazıcısı var:
> `internal/domain/tenant/rulewriter.go:964` (`AppendPolicyVersion`, layer `tenant`)
> ve `internal/domain/checkin/policyset.go:423` (`EnsureBaselinePolicyVersion`,
> layer `baseline`).
>
> ⚠️ **BURADA BİR `grep` SAYISI YAYIMLANMIŞTI VE HİÇBİR YOLDAN ÜRETİLEMİYORDU** (D52,
> 3. tur). Yayımlanan çıktı *"4 satır, ikisi çağrı ikisi `internal/store` tanımı"*
> idi; cümle kendi içinde çelişiyordu, çünkü `grep -v /internal/store/` filtresi
> tam da o iki tanımı **eler**. Yeniden ölçüldü (2026-08-20, bu makine):
> `grep -rn "AppendPolicyVersion(\|EnsureBaselinePolicyVersion(" --include='*.go' . |
> grep -v /internal/store/` →
> **POSIX `grep` ile (`/usr/bin/grep`) 2 satır** (yollar `./` önekli, filtre tutuyor:
> `./internal/domain/tenant/rulewriter.go:964` ve
> `./internal/domain/checkin/policyset.go:423`) ·
> **PATH'teki `grep` ile — bu makinede `ugrep 7.5.0` — 6 satır** (yollarda `./` yok,
> filtre **hiç** tutmuyor, iki tanım ve iki `querier.go` arayüz satırı da geliyor).
> **4 ikisinden de çıkmıyor.**
>
> 🔴 **DERS: KABUK SAYISI KAPI DEĞİLDİR.** Sayı hangi `grep`'in kurulu olduğuna
> bağlıydı, yani okuyan kişi ile yazan kişi farklı sayı görüyordu. Bu yüzden
> yazıcıların sayısı artık **karttan değil kapıdan** okunur:
> `TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed` yazıcı
> kümesini `db/`'den **türetir** ve her koşuda **yazdırır** — yukarıdaki iki ad, o
> kapının çıktısıdır, bir kabuk komutunun değil.
> · `copyOfShipped` (`rulewriter.go:1040`) `copied := st` ile ifadeyi **bütün**
> kopyalıyor; yalnız `Sid` ve `Resource` değişiyor.
> · `AuthorCommand`'ın alanları: `TenantID · Actor · PolicyID · Name · BasedOn ·
> Venues`. **`Reason` yok.**
> · Panel formu (`policyactions.go`) `op · policy · resource · name · based_on ·
> venue` okuyor; **doküman yükleme yok**.
> · İddia edilen çift (`reason="network proof of place…"` **ve** `ip_match=false`)
> `copyOfShipped`'den **üretilemez**, çünkü koşul da birlikte kopyalanıyor
> (`baseline.go`, `Condition{OpBool:{CtxTapIPMatch:true}}`).
>
> **Yapılan — ve ayrım korundu.** İki cümle de yazıldı: **(1)** politika cümleleri
> **bizim** (tenant ifadeyi *seçip kapsamlandırıyor*, metnini yazmıyor); **(2)** ama
> **kararı** tenant'ın kuralı vermiş olabilir ve `policy_layer='tenant'` bunu söyler —
> `NoteIsTenants` **onu** işaretliyor. **Etiket ve mekanizma korundu, yorumlar
> düzeltildi.** Düzeltilen yerler: `web/templates/pages/view.go` ·
> `web/templates/components/docketview.go` (iki blok) ·
> `web/templates/components/docket.templ` · `internal/domain/ledger/ledger.go`
> (üç blok) · `db/queries/transactions.sql` · `db/queries/reviews.sql` (+ `make sqlc`
> çıktısı) · `internal/handler/transactions_test.go` ·
> `internal/domain/ledger/mapper_test.go` · ADR 0005 · bu kart.
>
> 🔴 **METİN TARAYAN KAPI SİLİNDİ, YERİNE İKİ DAVRANIŞSAL/YAPISAL KAPI KONDU.**
> Silinen metin tarayıcısı bugün **doğru** olan bir
> cümleyi yasaklıyordu; korunacak gerçek şey cümlenin **kendisi** değil onu doğru kılan
> **yazma yolları**. Yerine:
> · `TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs`
> (`internal/domain/tenant`) — üreticiyi **çağırıyor**, `AuthorableRules()`'un
> döndürdüğü **her** belgenin **her** ifadesini
> alan alan karşılaştırıyor (`Reason`/`Effect`/`Action`/`Condition` aynı, `Sid` ve
> `Resource` değişmiş), ve *"hiç `reason` yoktu"* şeklinde boş geçmemesi için
> **kontrolü** var.
> · `TestAuthorCommand_CarriesNoProseDestinedForAStatement` — alan kümesi tam
> karşılaştırılıyor (ad içinde "Reason" aramak yerine), çünkü bir sonraki alan
> `Message` diye adlanabilir.
> · `TestPolicyStatement_HasExactlyOneProseField` — düzyazı taşıyan **tek** alanın
> `Reason` olduğunu tutuyor.
> · `TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument` (`cmd/tappa`)
> — belgeyi yazan üretim çağrılarını **ve** ham `INSERT INTO … policy_versions`
> bypass'ını tarıyor. ⚠️ **3. turda bu tek kapı üçe bölündü ve iki kaçış kapatıldı**
> — aşağıdaki D51 bloğuna bakınız.
>
> 🔴 **İLERİYE DÖNÜK RİSK — SAYILDI, SİLİNMEDİ.** Q22 v1 editörünü bilerek **form-only**
> yaptı ve ham-JSON editörünü **M9-07**'ye erteledi (`AuthorCommand`'ın kendi doc
> yorumu). O editör gelirse `reason` **yazılabilir** olur ve çürütülen iddia **doğru
> hâle gelir**. ⚠️ **Burada *"hangisi olursa olsun"* yazıyordu ve 3. turda yanlışlandı**
> (D51): kapılar o gün **üç şekli göremiyordu**. Editörün şekline göre hangi kapının
> kırmızı verdiği artık **tek tek sayılıdır** — `copyOfShipped` üzerinden giderse
> `TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs`; `db/queries`'e sorgu
> eklerse `TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed`;
> `internal/store`'a elle metot yazarsa
> `TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName`; yeni bir dosyadan
> çağırır ya da `INSERT`'ü sevk edilen Go'ya koyarsa
> `TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument`. **Dördü de
> enjekte edilip kırmızı ölçüldü** (2026-08-20). Göremediği şekil de yazılı:
> çalışma anında **sabit olmayan** parçalardan kurulan SQL. Bu **bugünkü olguyla
> karıştırılmadan** yazıldı.
>
> **(D47 — BLOKLAYAN) ETİKETİN NE YAKALADIĞI VE NE KAÇIRDIĞI — ÖLÇÜLDÜ.**
> Yalan söylediği **ölçülmüş** tek not sınıfı **T40**'tır ve o cümle **bizim
> baseline'ımızındır** (`base:ip-or-gps-ok` → *"network proof of place: the source IP
> matches the location"*), yani `policy_layer='baseline'` → **`NoteIsTenants` onu
> işaretlemiyor.** Yerel `tappa` DB'sinde, `BEGIN … ROLLBACK` içinde (2026-08-20):
> ```
> locations total ................................................. 344 082
> venues holding a list the guard now refuses ......................      66
> transactions at those venues .....................................      11
>   of those, ip_match=TRUE ........................................       4
>   of those, note LIKE 'network proof of place%' ..................       4
>   of those, policy_layer='tenant' (yani etiketlenirdi) ...........       0
> ```
> Ve tüm tabloda `policy_layer='tenant'` satır sayısı **0**'dır (`(null)` 186 258 ·
> `baseline` 133 154 · `guardrail` 110 422). **Etiket bu veritabanında hiçbir satırı
> işaretlemiyor** — çünkü işaretleyeceği şey henüz üretilmiyor
> (`TestPolicySetDB_TenantLayerIsEmptyToday` bunu zaten söylüyor).
> **İki şey ayrı ayrı yazıldı:** **(a)** FAZ B2'nin `netx` düzeltmesinden sonra bu sınıf
> **yeni satırlarda üretilemiyor** — yazma tarafında `netx.TooWideForProofOfPlace`
> reddediyor, okuma tarafında `tap.ipMatches` aynı fonksiyonu çağırıyor
> (`TestDecide_AStoredRangeTooWideForProofOfPlaceDoesNotMatch`); **(b)** ama
> **yukarıdaki 4 satır `transactions`'ta değişmez duruyor** (§4.3) ve o cümleyi sonsuza
> kadar taşıyor. Bu ayrım `ledger.Record.NoteIsTenants`'ın ve
> `components.DocketView.NoteIsTenants`'ın yorumlarına **yazıldı**.
>
> **(D48 — ORTA) KAÇIŞ ATFI YANLIŞTI; ÖLÇEREK *"gerçek test yaz"* SEÇİLDİ.**
> `TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered` düşman metni
> **`transaction_reviews.note`**'a POST ediyor ve yalnız `d.ReviewNote`'un basıldığı
> kartı okuyor; `d.Note` ve `v.Note` için düşman girdi taşıyan test **yoktu**. Özellik
> tutuyordu (`templ.EscapeString` üç interpolasyonda da var) ama atıf yanlıştı — bir
> özelliğin *kimse fark etmeden* denetlenmez hâle gelme şekli budur. Yazıldı:
> `TestPolicyNote_IsEscapedOnBothSurfacesThatPrintIt` — aynı `hostileNote` fikstürünü
> **fişten** (`d.Note`, `panelBrowserWith`) **ve tap sonuç ekranından**
> (`v.Note`, `mustBody`) geçiriyor, ham `<script>`'in **yokluğunu** ve
> `&lt;script&gt;`'in **varlığını** ikisinde de ölçüyor (ikinci yarı olmadan test
> yanlış yüzeyi ölçüyor olabilirdi). §9: ekranın HTML'i **okundu**, ekran
> **değiştirilmedi**.
>
> **(D49 — ORTA) `make check` SCRIPT'İ ÖNCE KOŞTURUP KAPILARINI SONRA DOĞRULUYORDU —
> SIRALAMAYA DEĞİL ÇAĞRIYA BAĞLANDI.** Ölçüm doğruydu:
> `TestDbReset_RefusesADaemonOnAnotherMachine` `t.Parallel()` **çağırmıyor** (seri faz),
> `TestDbReset_KeepsItsStructuralGuards` çağırıyor (ertelenir). Sıralamayı düzeltmek
> **yetmezdi**: `-run` ile tek test koşturulduğunda ya da `-shuffle` altında yine
> bozulurdu. Yapısal kontroller `dbResetStructuralProblems(text)` fonksiyonuna
> çıkarıldı; her iki test de onu **çağırıyor** ve davranışsal olan `exec.Command`'dan
> **önce** `t.Fatalf` veriyor. 🔴 `scripts/db-reset.sh` **elle koşturulmadı**.
>
> **(D50 — DÜŞÜK) ÜÇ YENİDEN ÜRETİLEMEYEN KAYIT — ÜÇÜ DE GERÇEĞE EŞİTLENDİ.**
> **(1)** `scriptguards_test.go`'nun yayımladığı `rg … -> "zero"` çifti `239d427`'de
> **2 satır** döndürüyordu (`internal/db/viewsecurity_test.go:17`, `:20` — ikisi de
> **yorum**). Esas iddia doğru, kanıtı değildi: iki komut da **verdikleri çıktıyla**
> yazıldı (`git grep -ln 'exec.Command("bash"' 239d427 -- '*_test.go'` →
> `cmd/rotatekek/script_test.go`) ve *"bir AD bir KOŞU değildir"* ayrımı açıkça kondu.
> **(2)** `scripts/db-reset.sh`'in sızıntı kaydındaki `probeleak_probeleak-pgdata`
> compose'un **hiç üretmediği** bir addır. Kural `<project>_<volume-key>` ve anahtar
> `tappa-pgdata`; bu makinede `docker volume ls` → **`tappa_tappa-pgdata`**,
> `docker network ls` → **`tappa_default`** (ölçüldü, salt-okuma). Doğru adlar yazıldı:
> `-p tappa` (script'in daima pinlediği) → `tappa_default` + `tappa_tappa-pgdata`, yani
> `make up`'ın zaten sahip olduğu nesneler; `-p probeleak` → `probeleak_default` +
> **`probeleak_tappa-pgdata`**. Yanlış adın operatöre maliyeti de yazıldı.
> **(3)** `docket.templ`'in *"a page that routinely holds 1 628 of them"* cümlesi tek
> seferlik bir **seed** EXPLAIN ölçümünü üretim iddiasına çeviriyordu; sayı
> **kaldırıldı** (kaynağında, `db/queries/transactions.sql`'de, *"seed tenant"* atfıyla
> duruyor — orası ölçümün kendisi).
>
> ⚠️ **KAPATILAMAYAN / DOĞRULANAMAYAN — bu turda yok denmedi:** kabul kriteri 4'ün
> **replay denemesi gerçek etiketle** maddesi hâlâ **bloke** (NTAG yazıcı donanımı yok;
> M8-05 FAZ B'ye bağlı) — D43'ün altında yazılı ve değişmedi.

> **Kart düzeltmesi (2026-08-20, M8-04 FAZ B3 — DÜZELTME TURU 3).**
>
> 🔴 **BU TURUN DERSİ: BİR KAPI, YANINDA YAZILI GARANTİYİ TUTMUYORDU.** 2. tur
> *"belgeyi yazan **her** yazıcıyı bildiriyorum — store'dan geçse de doğrudan tabloya
> vursa da"* diye yazdı; 3. turun denetimi **üç kaçış derledi**, üçünde de `go build`
> geçti ve **dört kapı da yeşil kaldı**.
>
> **(D51 — BLOKLAYAN) ÜÇ KAÇIŞ, ÜÇÜ DE KAPATILDI — VE HER BİRİ ENJEKTE EDİLİP
> KIRMIZI ÖLÇÜLDÜ (2026-08-20).**
> · **(b) `db/queries/*.sql` HİÇ OKUNMUYORDU** — yürüyüş yalnız `.go` kabul ediyordu,
> yani `CLAUDE.md §6`'nın *"her sorgu burada yaşar"* dediği dizin taranmıyordu.
> Artık `db/` (queries **ve** migrations) taranıyor ve yazıcı kümesi **oradan
> türetiliyor**.
> · **(a) İKİ ADLIK İZİN LİSTESİ + `internal/store/`'un atlanması** — ad bilinmeyen
> bir üçüncü yazıcı iki kere görünmezdi. Ölçüt artık **ad değil davranış**:
> `policyDocumentWriters` bir izin listesi değil, `db/`'den türetilen kümeye karşı
> **doğrulanan bir iddia**; `internal/store` da atlanmıyor, sqlc'nin `-- name:`
> başlığına göre **atfediliyor**.
> · **(c) Ham SQL kontrolü satır bazlıydı** — `INSERT INTO` ile `policy_versions`'ı
> aynı satırda arıyordu. Go tarafı artık **satır değil sözdizimi** okuyor
> (`go/parser`): ham dize kaç satıra bölünürse bölünsün tek değerdir, `+` zinciri
> eşleştirmeden **önce** birleştirilir, yorum ise ayrıştırıcıya hiç verilmez.
>
> **Tek kapı üçe bölündü** (adları beş yerde anılıyor):
> `TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed` ·
> `TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName` ·
> `TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument`, artı negatif
> kontrol `TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport`.
>
> **Enjekte edilip ölçülen altı şekil — hepsi YENİ kapıda kırmızı, hepsi ESKİ kapıda
> yeşildi:** store'a elle metot (kendi `INSERT`'üyle) · aynısı **sahte `-- name:`
> başlığıyla** · üretilmiş sabiti çalıştıran ikinci fonksiyon · üretilmiş yazıcıyı
> saran fonksiyon · `db/queries`'e yeni `-- name:` sorgusu · sevk edilen Go'da iki
> satıra bölünmüş `INSERT` (ve iki satıra bölünmüş `+` zinciri).
>
> 🔴 **VE GARANTİNİN CÜMLESİ GERÇEĞE EŞİTLENDİ.** Beş yerdeki *"any writer /
> whichever way"* ifadeleri kaldırıldı; yerine **hangi şeklin hangi kapıyı kırmızıya
> çevirdiği tek tek sayıldı** ve **göremediği şekil de yazıldı**: çalışma anında
> **sabit olmayan** parçalardan kurulan SQL (`fmt.Sprintf("INSERT INTO %s", …)`).
> Ona karşı duran şey bu dosya değil, `CLAUDE.md §6` ve altı paketin `query_test.go`
> kemeridir. Sayılan diğer limitler: store'da **izin verilen bir adın başlığı taklit
> edilebilir** (bedeli ölçüldü: türetilmiş küme + yazıcı başına tek segment + sabit
> kullanım kontrolü aynı anda sağlanmalı, yani taklit gerçek sorgunun **yerine
> geçmek** zorunda ve `make gen` onu geri koyar) · DDL/trigger okunmaz · testler ve
> fixture'lar kapsam dışı (pozitif kontrol adıyla yazılı) · SQL blok yorumu
> ayıklanmaz (ölçüldü: `db/` altında `/* */` yorum **yok**).
>
> **(D52 — BLOKLAYAN) YAYIMLANAN `grep` SAYISI HİÇBİR YOLDAN ÜRETİLEMİYORDU.**
> Düzeltildi ve iki `grep` uygulaması için ayrı ayrı ölçüldü — D46 bloğunda,
> yukarıda. **Ders kartın kendisine yazıldı: kabuk sayısı kapı değildir**; yazıcı
> sayısı artık her koşuda kapının kendisi tarafından yazdırılıyor.
>
> **(D53 — DÜŞÜK) `1 628` TARİHLENDİ.** `db/queries/transactions.sql`'deki EXPLAIN
> ANALYZE kurulumu artık **2026-08-07 (M6-04, commit `2e7ec64`)** tarihini taşıyor,
> **seed** atfı korundu, ve sayının *"o gün ölçümün alındığı günün büyüklüğü"*
> olduğu — canlı bir sayım olmadığı — açıkça yazıldı. sqlc kopyaları `make sqlc` ile
> takip etti. `internal/handler/employees_test.go`'daki roster tarayıcı örneği de
> artık **ağaçta gerçekten duran** satırı alıntılıyor (⚠️ kapsam dışı tek dokunuş,
> gerekçesi: silinmiş metni alıntılayan bir kontrol, kontrol olmaktan çıkar).
>
> ⚠️ **BAYAT BİR CÜMLE DE DÜZELTİLDİ:** T31 bloğundaki *"`make audit` hâlâ kırmızı"*
> bugün yanlıştı — `go1.26.7`, `make audit` **exit 0**. Yukarıda yerinde düzeltildi.

**Kabul kriterleri.**
- agent `tappa-security-auditor` tam repo üzerinde koştu; R1–R8 için kanıtlı rapor.
- `make audit` temiz: `govulncheck` + `scripts/redline-check.sh`.
- KRİTİK ve YÜKSEK bulgu **kalmadı**; ORTA/DÜŞÜK olanlar ya kapandı ya
  gerekçesiyle kabul edildi ve yazıldı.
- Manuel doğrulamalar: replay denemesi gerçek etiketle, çapraz-tenant erişim
  denemesi, oturum çalma senaryosu, oran sınırı.
- Mekanik taramanın **kanıt olmadığı** unutulmadı — derin denetim ayrı yapıldı.

---

## M8-05 — Plaket encode runbook

- **Bağımlılık:** M2-01 · Q06 · Q10
- **Kırmızı çizgi:** §4.7
- **Commit:** `docs(ops): document tag encoding runbook`

**Amaç.** Fiziksel plaketleri üretime hazır hale getirmek.

> **Kart düzeltmesi (2026-08-18, M8-05 FAZ A uygulaması sırasında).** Altı kriter
> tek tek ölçüldü: **dördü** (1, 4, 5, 6) bugün donanımsız yapılabiliyor, **biri**
> (3) fiziksel çipe tamamen bloke, **biri** (2) ikiye bölünüyor — çipler
> kullanıcıda **var**, yazıcı **yok**, yani ölçüm bugün yapılır ama **seçim**
> donanıma bakar. Aşağıdaki tablo bunu satır satır gösteriyor.
> Kart bu yüzden **FAZ A / FAZ B** olarak bölündü (mercek ölçütü — agent-brief:
> bir görev iki farklı saldırıyla denetleniyorsa tek commit'te ikisi de yüzeysel
> kalır). Üç ayrı çelişki de düzeltildi:
>
> **(1) "SDM MAC input offset" bir SEÇİM DEĞİL, KARAR.** Kriter onu ayarlanacak
> bir alan gibi sunuyordu; [ADR 0003](../adr/0003-sdm-modu-ve-anahtar-yonetimi.md)
> madde 2 zaten sabitliyor: `SDMMACInputOffset == SDMMACOffset` → MAC girdisi
> **boş**. Kaynakta doğrulandı: NXP **AN12196 rev. 1.8** §4.4.4.2.1 tam olarak bu
> yapılandırmayı *"SDMMAC = MACt(KSesSDMFileReadMAC; **zero length input**)"*
> diye tanımlıyor. Kriter "yazılı olsun" değil "**ADR'den türetilmiş olsun**"
> demeli; runbook öyle yazıldı.
>
> **(2) "NXP TagWriter" dev akışı olarak YANLIŞ — ve yerine konacak şey bir uygulama
> değil, bir MİMARİ KISIT.** Ölçüldü: NTAG 424 DNA'da `ChangeFileSettings` (0x5F)
> ve `ChangeKey` (0xC4) **kimliği doğrulanmış bir oturum** ister (AN12196 **rev. 1.8**
> §6.9 tablo 19 / §6.16 tablo 26-27 — rev. 2.0'da **§5.9 tablo 18 / §5.16 tablo
> 25-26**; ikisi de `KSesAuthENC` + `KSesAuthMAC` + `TI` + `CmdCtr` taşır), ve o
> oturumun anahtarları çipin **canlı** ürettiği `RndB`'den doğar (rev. 1.8 §6.6
> tablo 14 — rev. 2.0'da **§5.6, tablo numarası yine 14**; bölüm kayıyor, tablo
> kaymıyor, ikisi ayrı aranmalı).
> Dolayısıyla **"offline APDU script'i üret, sonra çipe yapıştır" mümkün değildir**;
> encode aracı canlı bir taşıma katmanı taşımak zorundadır — yani
> `test/fixtures/seedkeys` + `cmd/rotatekek` emsalinin (saf filtre, sürücü yok)
> bu görevde **uygulanamayacağı** anlamına gelir.
>
> ⚠️ **Bu maddedeki iki cümlenin kanıt gücü AYRIDIR ve karıştırılmamalıdır.**
> APDU/oturum kısıtı **güçlü**: AN12196'nın ilgili tablolarından birebir okundu ve
> sayfa numaralarıyla runbook'ta duruyor. Buna karşılık **araç iddiaları zayıf** —
> TagWriter'ın anahtar değiştirmediği ve TagXplorer'ın desteklenmediği yalnızca
> **arama sonucu özetlerinden** okundu, ilgili sayfaların bir kısmı **403**
> döndürdüğü için birincil kaynaktan **doğrulanamadı**. Kriter "TagWriter" yerine
> **araç yolu FAZ B'de seçilir** diye yazıldı.
>
> 🔴 **VE BU TUR BİR YANLIŞ ATIF DÜZELTİYOR:** bu blok daha önce *"iki yol ve
> maliyetleri runbook'ta ölçülü duruyor"* diyordu. **Ölçüldü, yanlıştı** —
> `grep -nEi "yol a|yol b|tagwriter|tagxplorer|nfc\.cool|rfiddiscover|pegoda|euro"`
> runbook'ta **sıfır** isabet veriyordu: ne karşılaştırma, ne maliyet, ne araç adı.
> Bugün runbook'ta **iki yolun ŞEKLİ** yazılı (*"Araç yolu — KARAR DEĞİL, KARAR
> ÖNERİSİ"*), zayıf iddialar **zayıf diye etiketli**, ve **maliyet hâlâ ölçülmedi**
> — ölçülmediği de orada yazıyor. Seçim **kullanıcınındır** (yeni bağımlılık,
> CLAUDE.md §1).
>
> > 🔴 **Kart düzeltmesi (2026-08-20).** Yukarıdaki *"maliyet hâlâ ölçülmedi"*
> > **artık yanlış** ve *"iki yol"* da eksik. Kullanıcının *"önce ÖLÇ, sonra karar
> > ver"* talimatıyla **dört yol** ölçüldü ve `deploy/README.md` → *Plaket encode*
> > → **"Dört yol — ÖLÇÜLDÜ (2026-08-20)"** alt bölümüne yazıldı: **A** USB
> > okuyucu + Go (**€42–48**, 1 Go modülü) · **B** kendi Android app'imiz (**€0**,
> > yeni dil zinciri) · **C** kendi iOS app'imiz (**$99/yıl** + macOS) · **D**
> > üçüncü parti (**$29,99/yıl**, 🔴 **ADR 0003 md.5 ihlali**).
> > 🔴 **VE ÖLÇÜM BU KARTIN ÜÇ CÜMLESİNİ ÇÜRÜTTÜ:** *(i)* *"hazır uygulamalar
> > `ChangeKey` yapamaz"* — NXP TagWriter `ChangeFileSettings`/SDM'i **yapıyor**
> > (kılavuz Rev 1.29 §4.3–4.7), `ChangeKey`'i yapmıyor; ve **NFC.cool Tools
> > telefonda ikisini de yapıyor**. Ama hiçbiri işe yaramıyor ve sebebi **protokol**:
> > `ChangeKey` gövdesi `Eski ⊕ Yeni ‖ KeyVer ‖ CRC32`, onu kuran taraf düz anahtarı
> > **görmek zorunda**. *(ii)* *"NXP'nin resmî yolu RFIDDiscover + PEGODA"* — AN12196
> > rev. 1.8 **ve** 2.0'ın kişiselleştirme bölümü **hiçbir araç adı vermiyor**;
> > `PEGODA` ve `TagXplorer` iki revizyonda da **sıfır kez** geçiyor. *(iii)* Asıl
> > eksen *"araç hangi cihazda koşuyor"* değil, **APDU RÖLESİ mümkün mü** — çipe
> > dokunan taraf yalnız bayt taşır, kripto sunucuda koşar, düz anahtar sunucudan
> > **çıkmaz**. `ISO 14443-4`'te `FWT` **PICC'in** yanıt gecikmesini sınırlar,
> > PCD'nin düşünme süresini değil → araya ağ turu koymak standardı bozmuyor. Bu
> > sayede **A, B ve C'nin üçü de ADR 0003 md.5'i tadil etmeden** sağlıyor.
> > **Kararı belirleyen tek ölçüm yapılmadı:** iOS'un **~20 sn sert** bağlantı
> > sınırının altında tam bir röleli tur yetişiyor mu (kaba hesap 17 tur × ~150 ms
> > ≈ 2,6 sn diyor, **silikonda ölçüm yok**). Yetişirse C ile B aynı sınıfa çıkar;
> > yetişmezse seçim **A ile B** arasına iner. **Hiçbir çip encode edilmedi.**
>
> **(3) "Uçtan uca doğrulama"nın NEYİ kanıtlaması gerektiği eksikti — ve bu artık
> boş bir uyarı değil, ADLANDIRILMIŞ bir şüphe.** ⚠️ **Bu bloktaki bölüm/tablo
> numaraları AN12196 rev. 1.8'indir; rev. 2.0 karşılıkları parantezde.** FAZ A'da
> AN12196'nın **yayımlanmış known-answer vektörü** (rev. 1.8 §4.4.4.2.1 — rev. 2.0:
> **§3.4.4.2.1**) Tappa'nın zincirine karşı koşuldu ve
> **birebir tuttu** — SV2 düzeni, boş mesaj CMAC'i ve tek-indeksli 8 baytlık
> kısaltma, belgenin yayımladığı `SDMMAC` değerini **üretti** (değer buraya
> yazılmıyor; §4.7 ruhu — kaynak: AN12196 rev. 1.8 §4.4.4.2.1 **tablo 5** adım 14;
> rev. 2.0'da aynı örnek **§3.4.4.2.1 tablo 4**, adım numarası aynı).
> Ama aynı belge **plain** mirroring için `SDMReadCtr`'ın SV
> girdisine **LSB-first** girdiğini söylüyor (rev. 1.8 §4.3 tablo 2, adım 4 —
> rev. 2.0: **§3.3 tablo 1**) ve aynı UID
> için rev. 1.8 §4.4.1'in (rev. 2.0: **§3.4.1**) URL'i `ctr=000001` gösteriyor —
> yani **URL metni ile SV baytları
> ters sırada**, oysa `sv2()` o gün URL baytlarını **verbatim** kullanıyordu.
>
> ✅ **BU ÇELİŞKİ ARTIK KAPANDI — M2-08, donanımsız.** FAZ A bunu *"FAZ B'nin ilk
> işi"* diye devretmişti; devir **gerçekleşmedi çünkü gerekmedi**. `sv2()` düzeltildi
> ve dış kaynaklı KAT vektörleriyle çivilendi (`internal/sun/an12196_kat_test.go`).
> Ayrıntı: [m2-sun.md](m2-sun.md) → M2-08 · ADR 0003 ek notu · runbook'un
> *"`ctr` bayt sırası"* kutusu.
>
> ⚠️ **Ve kapanış FAZ B'nin KAPSAMINI değiştirdi — bu, kaydedilmeden geçmemeli.**
> FAZ A metni vektörlerin kusuru *"göremediğini"* söylüyordu; M2-08'de ölçülen
> daha ağır: `internal/sun/verify_mac_test.go` içinde adında *verbatim* geçen bir
> SV2 testi vardı, kendini *"the load-bearing anti-reversal test"* diye tanıtıyordu
> ve **doğru düzeltmeyi yapısal olarak yasaklıyordu**. Yani gerçek çip gelseydi bile
> doğru yamayı yazan kişi kırmızı bir test görecekti — **donanım tek başına bu
> maddeyi kapatmazdı**. Sonuç olarak FAZ B devir listesinin **1. maddesi silinmedi,
> DARALTILDI**: kalan iş yalnız **encode** tarafının (URL'ye MSB-first yazma)
> gerçek silikonla doğrulanmasıdır ve artık **ilk sırada koşulması gerekmiyor**.
>
> Ek olarak **Q10 karara bağlandı** (orkestratör, 2026-08-18): plaketleri
> **kendimiz encode ederiz**. Gerekçe runbook'ta (`deploy/README.md` → "Plaket
> encode"), `open-questions.md`'de değil — orası orkestratörün.

> **Kart düzeltmesi (2026-08-20, M8-05 FAZ B1 TUR 1 sırasında).** Üç cümle
> değişti; hiçbiri silinmedi, üçü de burada.
>
> **(a) ARAÇ YOLU ARTIK SEÇİLDİ.** Kullanıcı 2026-08-20'de **B yolunu** (kendi
> Android uygulamamız, **APDU rölesi** şeklinde) seçti; kripto sunucuda koşar,
> düz anahtar sunucu sürecinden çıkmaz. Kararın kendisi, sınırı, tehdit modeli,
> oturum durumunun nerede yaşadığı, anahtar numaraları ve **yarım-yazma kurtarma
> yolu** [ADR 0017](../adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)'dedir.
> ⚠️ Onay **Android dil zinciri** içindir; **yeni bir Go bağımlılığına onay
> verilmedi** (§1). Kod hâlâ yazılmadı, çip hâlâ encode edilmedi.
>
> 🔴 **Bunun ölü bıraktığı cümleler — sayıldı, ve BU KARTTA İKİ SATIR:**
> ⚠️ **Bu bloğun ilk hâli kendi sayımıyla çelişiyordu** (*"ikisi bu kartta"* dedi
> ama biri `deploy/README.md`'deydi; *"kalan üçü"* dedi ama dört madde saydı).
> Düzeltilmiş sayım:
> **(i)** aşağıdaki **FAZ A kriter 2** satırı (`m8-deploy-pilot.md`) **üç** ölü
> ifade taşıyor — *"maliyet ölçülmedi"* (2026-08-20'de ölçüldü) · *"araç iddiaları
> zayıf kanıt diye etiketli"* (üçü de kapandı, ikisi çürüdü) · *"Seçim FAZ B'de ve
> kullanıcınındır"* (seçim yapıldı). Satır tarihsel kayıt olarak **duruyor**.
> **(ii)** aşağıdaki **FAZ A kriter 1** satırı: *"yazma izni anahtarla kilitli …
> ADR'den normatif türetildi"* — **hangi anahtara** kilitleneceği bu turda
> **karara bağlanmamıştı** ve elde olan tek anahtar halka açık fabrika anahtarı
> 0'dır; skill'e tam o cümle için bir uyarı eklendi (ADR 0017 §5.0 · ADR 0005
> risk 8).
> **(iii)** `deploy/README.md`'de **beş** ölü cümle var ve **beşine de** tarihli
> şerh düşüldü: *"Araç yolu — KARAR DEĞİL, KARAR ÖNERİSİ"* başlığı ·
> *"BU BAŞLIK ALTINDA HİÇBİR ŞEY SEÇİLMEDİ"* · *"Dört yol — … KARAR YİNE
> VERİLMEDİ"* başlığı · *"VE BURADA HÂLÂ HİÇBİR ŞEY SEÇİLMEDİ"* · **FAZ B devir
> listesi md. 2** (*"Araç yolu seçilir ve kurulur"* — o liste **bu kartta değil**,
> kart ona yalnız atıf yapıyor). Bu sayı ADR 0017'nin verdiği **beş** ile
> **aynıdır**; ilk hâlinde değildi.
> ⚠️ İlk sürümü bu blok *"Hiçbir şey seçilmedi"*yi **bu kartın** nested bloğuna
> atfediyordu; ölçüldü, o cümle bu kartta **hiç yok** — düzeltildi.
>
> **(b) FAZ B'nin ENCODE YARISI SANILDIĞI KADAR ÇIPLAK DEĞİL.** Kart ve runbook
> uçtan uca doğrulamayı yalnız silikona bağlıyordu; ölçüldü, AN12196'nın
> **kişiselleştirme bölümü çalışılmış örnekler taşıyor** — rev. 1.8 §6.6 tablo
> 14 (`AuthenticateEV2First`), §6.9 tablo 19 (`ChangeFileSettings`), §6.16 tablo
> 26/27 (`ChangeKey`); rev. 2.0'da §5.6 tablo 14 / §5.9 tablo 18 / §5.16 tablo
> 25/26. Üçü de somut `RndA`/`RndB`/`TI`/oturum anahtarı/şifreli gövde/MAC
> değerleri veriyor, yani **komut formatı tablosu değil, known-answer
> vektörü**. 🔴 **Ama kapatmadıkları iki eksen var ve ikisi de M2-08 sınıfı:**
> *(i)* yayımlanmış `ChangeFileSettings` örneği **şifreli-PICC** yapılandırmasını
> kuruyor, Tappa'nın **plain**'ini değil — plain gövdenin alan sırası yalnız
> **NT4H2421Gx** veri sayfası §10.7.1 tablo 69'dan okunur; *(ii)* iki `ChangeKey`
> örneğinin de eski anahtarı **sıfır**, yani `Old ⊕ New == New` → **XOR'un
> yapılıp yapılmadığını hiçbir vektör ayırt etmiyor**.
>
> **(c) VERİ SAYFASI BUGÜN ERİŞİLEBİLİR.** Runbook'un *"Ölçemediklerim"*
> maddesi 3 *"veri sayfasına birincil kaynaktan erişilemedi (nxp.com JS'siz
> istemcilere 404 döndürüyor)"* diyor. Bugün ölçüldü: `nxp.com/docs/en/data-
> sheet/NT4H2421Gx.pdf` düz bir `curl`'e **HTTP 200** ve **gerçek PDF** döndürdü
> (rev. 3.0, 2019-01-31, 97 sayfa). O madde **bu belge için** bayattır; araç
> sayfalarındaki 403/404 gözlemleri hakkında bir şey söylemiyor.

**FAZ A — donanımsız yarı (bu tur).**

| # | Kriter | Durum |
|---|---|---|
| 1 | Encode ayarları yazılı: UID mirror + counter mirror açık, **MAC girdisi boş** (ADR 0003 md. 2), dosya okuma izni açık, **yazma izni anahtarla kilitli** | ✅ bugün yapılabilir — runbook'ta, ADR'den normatif türetildi |
| 2 | Geliştirme akışı: 10'luk NTAG 424 DNA paketi (~30 €) + **araç yolu** | ⚠️ **yarısı** — paket kullanıcıda **VAR**, yazıcı **YOK**. Runbook iki yolun **ŞEKLİNİ** ve **kısıtını** yazıyor; **maliyet ölçülmedi**, araç iddiaları **zayıf kanıt** diye etiketli. **Seçim FAZ B'de ve kullanıcınındır** (§1). |
| 3 | **En az bir fiziksel etiketle uçtan uca doğrulama** | 🔴 **donanıma bloke** → FAZ B |
| 4 | Anahtar teslimi ve döndürme prosedürü | ✅ bugün yapılabilir — runbook'ta; `retire + replace` ile KEK rotasyonu **ayrı tutuldu** |
| 5 | Anahtarlar repoya/sohbete/e-postaya yazılmadı; KEK ile sarmalı DB'de | ✅ bugün yapılabilir — **mekanizma olarak** yazıldı (`sun.Wrap`/`sun.Zero`, R7 kuralları, `seedkeys` emsali) |
| 6 | Plaket baskısı: A5, NFC + QR birlikte, kamera görüş alanına montaj notu | ✅ bugün yapılabilir — skill `tappa-brand` → "Plaket baskısı"na atıfla; QR'ın §5 sonucu yazıldı. ⚠️ **A5 skill'de YOK** (ölçüldü: `A5`/`148`/`210` sıfır isabet); kâğıt boyu **handoff.md**'den gelir, skill yerleşim/QR ölçüsü verir. |

**FAZ B — fiziksel çip gerektiren yarı (devir).** **Dokuz** maddelik yükümlülük
listesi runbook'un sonundadır: `deploy/README.md` → **"FAZ B'ye devredilenler"**.
Kriter 2'nin araç seçimi ve kriter 3'ün tamamı oraya aittir.
⚠️ **Liste 2026-08-20'de sekizden dokuza çıktı** (ADR 0017): md. 6 anahtar
numarasına göre ikiye ayrıldı, md. 7 kimlik doğrulanmış yazma denemesini de
kapsayacak şekilde genişledi, ve **md. 9 eklendi** — anahtar 0 maruziyeti +
*"anahtar 0 fabrika varsayılanındayken plaket duvara çıkamaz"* güvenlik çizgisi
(bu bir **pilot** bloklayıcısıdır, encode bloklayıcısı değil).
⚠️ **Listenin 1. maddesi M2-08 ile DARALDI ve sıra ayrıcalığını kaybetti.** Eskiden
*"`ctr` bayt sırası — diğer yedisinden önce koşulur"* diyordu; o gerekçe (*yanlış
tarafta duran kod her tap'i reddeder*) **decode** tarafı içindi ve decode tarafı
artık kapalı. Kalan iş **encode** tarafının doğrulanmasıdır: encode aracı sayacı
URL'ye gerçekten **MSB-first** yazıyor mu. Bu, kalan **sekiz** maddeyle **aynı
turda** ölçülebilir; ayrıca `ctr` palindrom olmadığı için sinyal gürültüsüzdür.
(Liste 2026-08-20'de dokuza çıktı; bu cümle *"yedi"* diyordu.)

> **Kart düzeltmesi (2026-08-21, M8-05 FAZ B2a — EV2 kripto çekirdeği).**
>
> **Sevk edilen (yalnız saf kripto, ADR 0017 turun 2'sinin ilk kod turu):**
> `internal/sun/ev2.go` (AuthenticateEV2First'ün iki yarısı · oturum anahtarı
> türetimi · CommMode.Full sarmalayıcısı: IV, şifreleme, komut/yanıt MAC'i,
> kısaltma, ISO 9797-1 pad/unpad) ve `internal/sun/changekey.go` (Tablo 63'ün
> **iki** gövdesi + `CRC32NK`). HTTP · DB · durumlu oturum · APDU taşıma **YOK**
> — onlar B2b. Yeni bağımlılık yok (`crypto/aes`, `crypto/cipher`, `hash/crc32`,
> `encoding/binary` — hepsi stdlib). `internal/sun` kapsamı **%95,9**.
>
> 🔴 **BELGE İKİ YERDE KENDİSİYLE ÇELİŞİYOR VE İKİSİNİ DE KENDİ DEĞERİ ÇÖZDÜ.**
> İkisi de M2-08 sınıfıdır (bir alan sırası / bir uzunluk), ikisi de **ölçülerek**
> kapatıldı, ikisinin de üç adayı testte duruyor:
> 1. **AN12196 rev. 2.0 §5.8.2 Tablo 17** (rev. 1.8 §6.8.2 Tablo 18): adım 4 ve 12
>    `CmdHeader`'ı `02 000000 **530000**` yazıyor, C-APDU (adım 15) `**800000**`
>    taşıyor. Adım 13'ün yayımlanmış CMAC'i **yalnız `800000`** ile üretiliyor
>    (`A8D185D9…`); `530000` başka bir değer veriyor. Kripto kazandı.
> 2. **AN12196 rev. 2.0 §6.3 Tablo 28** (rev. 1.8 §7.3 Tablo 29): adım 13 yanıt
>    MAC girdisini *"Status || **Cmd** || CmdCounter+1 || TI || …"* diye
>    **etiketliyor**; NT4H2421Gx rev. 3.0 §9.1.9 yanıtlar için `Cmd` **saymıyor**.
>    Yayımlanmış CMAC (`F4593D5F…`) **`Cmd`'siz** okumayı seçiyor. Veri sayfası
>    kazandı.
>
> ✅ **ADR 0017 §6 md. 6 ARTIK ÖLÇÜLDÜ, YALNIZ İDDİA DEĞİL.** `ChangeKeyData`'dan
> XOR silindi (`newKey[i]^oldKey[i]` → `newKey[i]`) ve paket testleri koşturuldu:
> **tam bir test kırmızıya döndü** — sıfırdan farklı eski anahtarla kurulan
> `TestChangeKey_XORAppliesToANonZeroOldKey`. Yayımlanmış **her** vektör
> (iki uçtan uca `ChangeKey` C-APDU'su dahil) **yeşil kaldı**. Yani ADR'nin
> *"hiçbir dış vektör XOR'u ayırt edemez"* cümlesi bir deney olarak yeniden
> üretildi. ⚠️ O testin beklenen değeri **DERIVED, NOT TRANSCRIBED**'dır ve dosyada
> öyle etiketlidir; yayımlanmış vektör gibi sunulmuyor.
>
> ✅ **`CRC32NK`'nın serileştirmesi KAPANDI** (md. 6'nın kapatılabilir yarısı).
> Dört makul diziliş testte `hash/crc32`'den bağımsız hesaplanıyor ve **yalnız
> biri** Tablo 25 adım 7'nin `789DFADC`'sini üretiyor: **tümlenmemiş (ham) register,
> LSB-first**. Tarif metne yazılmadı; belgenin değeri beklenen sabit alındı.
>
> **Mutasyon kanıtı: 11 bayt-sırası/uzunluk mutasyonu, 11'i de kırmızı.** CmdCtr'ın
> iki yerde MSB-first'e çevrilmesi · SV1/SV2'de `RndB` diliminin kuyruktan
> alınması · iki rotasyonun yön değiştirmesi · kısaltmanın çift indekse kayması ·
> pad'in "zaten 16'nın katıysa fazladan blok ekleme" kuralının düşürülmesi ·
> yanıtın `CmdCtr+1` yerine `CmdCtr` kullanması · CRC'nin MSB-first olması ·
> CRC'nin tümlenmiş register'ı kullanması · XOR'un silinmesi.
>
> 🔴 **AÇIK KALAN — ve B2b'nin işi:** ADR 0017 §6'nın md. 5 (anahtar 0'ın şeması) ·
> md. 7 (oturum TTL/süpürücü/`Zero` garantisi — bu tur yalnız `EV2Auth.Zero()`
> **sağladı**, çağrılacağı yeri değil) · md. 8 (`audit_log`) · md. 10 (yetkilendirme
> kapısı) · md. 12 (UID uzayı) · **md. 13** (`FileAR.Change`/`ReadWrite`).
> **Plain SDM `CmdData` düzeni** de açık — sarmalayıcı gövdeyi opak sayar,
> `ChangeFileSettings` gövdesi bu turda **kurulmadı** ve alan sırası yalnız
> NT4H2421Gx rev. 3.0 §10.7.1 Tablo 69'dan okunur; ⚠️ **ADR 0017 §6 onu ayrı bir
> madde olarak SAYMIYOR** — en yakın sayılı komşusu md. 13'tür. Ayrıca hâlâ
> **hiçbir çip encode edilmedi**.
>
> **Denetim turu düzeltmeleri (2026-08-21, bağımsız üçüncü göz).** Bloklayan bir
> bulgu ve altı bloklamayan; hepsi kapatıldı:
> - 🔴 **BLOKLAYAN:** `ev2.go`'nun iki `subtle.ConstantTimeCompare`'i
>   `cmd/tappa/constanttime_test.go`'nun **envanterine kaydolmamıştı** → `make check`
>   kırmızıydı. `"internal/sun/ev2.go": 2` eklendi, **ne karşılaştırdığı yazılarak**.
>   Ölçüm: 10 üretim dosyası, 16 karşılaştırma, **0 korumasız**.
> - 🔴 **Kapsam iddiası düzeltildi:** ilk tur 11 kapsanmamış bloğun **hepsini**
>   *"daha önceki uzunluk kontrolünün arkasında ulaşılamaz"* diye rapor etmişti;
>   **biri ulaşılabilirdi**. 4 baytlık bir gövde üzerine **geçerli MAC** taşıyan
>   yanıt çerçevesi MAC kapısını geçip hizalama hatasına düşüyor.
>   `TestEV2_UnwrapResponseFullRejectsAMisalignedCiphertext` (kontrol alt-testiyle:
>   MAC bozulunca **daha erken** düşüyor) eklendi. Kapsam **%95,4 → %95,9**; kalan
>   **10** blok bu kez **blok blok**, her birinin önündeki kapı adlandırılarak
>   gerekçelendirildi.
> - Kırmızı çizgi kural kimliği üç yerde **R7c → R7** (R7c GPS kuralıdır;
>   sabit-zaman R7'dir — emsal `an12196_kat_test.go` zaten doğrusunu yazıyordu).
> - Sayfa numarası çelişkisi: rev. 1.8 §7.3 **başlığı s.44**, **Tablo 29 s.45**;
>   dosya içi iki yer artık aynı şeyi söylüyor.
> - Veri sayfasının §8.2.4.6'sı anahtar sürümünü **"GetVersion"** ile geri
>   okutuyor; doğrusu §10.6.2 **GetKeyVersion** görünüyor — **etiketlendi, sessizce
>   düzeltilmedi** (bu tur hiçbir şeyi okumuyor, yalnız yazıyor).
> - `ev2SessionKeys` başarı yolunda da yerel CMAC dizilerini sıfırlıyor; dosya
>   başlığındaki sıfırlama iddiası artık **hangi tamponlar** olduğunu sayıyor.
>
> **Güvenlik denetimi turu (2026-08-21, `tappa-security-auditor` — VERDICT: ONAY,
> bloklayan yok).** Üç bloklamayan bulgu commit'ten önce kapatıldı, biri sayıldı:
> - 🔴 **F1 — `Zero` disiplini bir cümleydi, mekanizma değil.** Denetçi **on bir
>   `defer Zero(...)`'ın hepsini sildi** ve `go test ./internal/sun/` **tamamen
>   yeşil kaldı**; oysa `ev2.go`'nun başlığı listeyi *"so the claim can be checked"*
>   diye yazıyordu. Artık `TestEV2_ZeroDisciplineIsInventoried` kaynağı **go/ast ile
>   okuyup** dosya başına sayıyor (`ev2.go: 11`, `changekey.go: 1`) ve
>   **iki yönlü ratchet**: silmek de eklemek de kırmızı verir (ikisi de ölçüldü).
>   ⚠️ **Ne kanıtlamadığı testin kendi yorumunda yazılı:** bir wipe'ın
>   **VARLIĞINI** sayıyor, **doğru tamponu** sıfırladığını değil — ve **eksik**
>   olanı hiç bulamaz (F2 tam olarak eksik bir wipe'tı).
> - 🔴 **F2 — itiraf edilen alışkanlığın ikinci örneği bulundu ve kapatıldı.**
>   `ev2RotateLeft1(rndB)` sonucu **isimsiz** kullanılıyordu: fonksiyon taze 16 bayt
>   ayırıp **RndB'nin tamamını** yazıyor, `msg`'e kopyalanıyor, sonra
>   **sıfırlanmadan** düşüyordu → her el sıkışmada oturum anahtarı girdisinin
>   yarısının **silinmemiş ikinci kopyası**. Aynadaki `ev2RotateRight1` yalnız
>   **adlandırılmış bir yerele bağlandığı için** kapsanıyordu — gerekçelendirilmiş
>   değil, **tesadüfi** bir ayrım. `rot := …; defer Zero(rot)` eklendi.
>   **Aynı tarama tekrarlandı** (go/ast, anahtar malzemesi üreten 11 fonksiyonun
>   çağrı yerleri): iki dosyada **16 çağrının 16'sı da bir isme bağlı**, ikinci bir
>   örnek **yok**. Ayrıca dosya başlığına yeni bir mekanizma notu girdi: tüm
>   tamponlar **tam kapasiteyle** ayrılıyor, yani hiçbir `append` yeniden
>   ayırmıyor ve `Zero`'nun ulaşamayacağı yetim kopya kalmıyor.
> - 🔴 **F3 — all-zero (fabrika varsayılanı) `newKey` reddediliyor artık.**
>   `ChangeKeyData` uzunluğu doğruluyor, **değeri** doğrulamıyordu; 16 sıfır baytlık
>   bir `newKey` Tablo 63'e göre **kusursuz geçerli** bir gövde üretiyordu.
>   Ağır sonucu: satıra sarmalı bir sıfır anahtar yazılır, plaket *"encode edildi"*
>   işaretlenir, ve **§5.3 sonda 2 BAŞARILI döner** — yarım-yazma teşhisi bunu
>   **göremez**. Risk 8 (oltalama), **tespit sinyali olmadan**. İki kapı eklendi
>   (§8.2.4.2 ve §5.3 atıflarıyla): **all-zero `newKey`** ve **`newKey == oldKey`**
>   (no-op; §5.3 zaten *"kurtarma hiç `ChangeKey` çağırmaz"* diyor). İkisi de
>   `subtle.ConstantTimeCompare` ile ve **envantere yazıldı**
>   (`internal/sun/changekey.go: 2`; toplam 11 dosya, 18 karşılaştırma, 0 korumasız).
>   ✅ **Meşru vaka engellenmiyor — ölçüldü:** ağaçtaki hiçbir çağıran sıfır anahtar
>   ya da no-op yazmıyor; AN12196'nın iki yayımlanmış vektörü de sıfırdan farklı ve
>   birbirinden farklı anahtarlar kullanıyor, **all-zero ESKİ anahtar** (boş çip,
>   normal vaka) **kabul edilmeye devam ediyor** — testte ayrı bir kontrol var.
>   ⚠️ `newKey == oldKey` reddi bir **karar**, protokol değil: Tablo 63 buna izin
>   veriyor. Geri alınabilir; gerekçesi testin yorumunda.
>
> 🔴 **F4 — B2b'YE DEVREDİLEN, KAPATILMADI (denetimde sayıldı):**
> 1. **`RndA`'nın üretimi çağıranındır ve bu bilinçli** — **bu iki dosyada**
>    (`ev2.go`, `changekey.go`) ne `crypto/rand` ne `math/rand` var, çünkü KAT'lar
>    belgenin sabit `RndA`'sını besleyebilmek zorunda.
>    ⚠️ **DÜZELTME (2026-08-21, 3. denetim):** burada *"bu **pakette**"* yazıyordu
>    ve **olgusal olarak yanlıştı** — `internal/sun/keys.go:6` `crypto/rand`
>    **import ediyor** ve `:86` `rand.Read(nonce)` çağırıyor (GCM nonce'u, ADR 0003
>    md. 4). Yanlış olan **ölçüm cümlesiydi**; aşağıdaki normatif talimat
>    etkilenmiyor. B2b **`crypto/rand` ile üretmek, hatayı kontrol etmek ve ASLA
>    tekrar kullanmamakla** yükümlüdür.
>    🔴 **Tekrar kullanımın bedeli bir gizlilik kaybı değil, bir TEŞHİS
>    YANILGISIDIR:** kaydedilmiş bir `(E(K,RndB), E(K,TI‖RndA'‖caps))` çifti, aynı
>    `RndA` tekrarlandığında RndA-echo kapısını **anahtar olmadan** geçirir → ADR
>    0017 §5.3 **sonda 2 sahte bir "başarılı"** verir.
> 2. **`EV2Auth`'un BEŞ alanının beşi de ihraç edilmiş** (`TI`, `KeyENC`,
>    `KeyMAC`, `PDcap2`, `PCDcap2` — `ev2.go:158-173`) **ve *"kimlik doğrulandı"*
>    işareti taşıyan gizli alan YOK.** ⚠️ **DÜZELTME (2026-08-21, 3. denetim):**
>    burada *"üç"* yazıyordu; maddenin özü doğrulandı, yalnız **sayı** yanlıştı.
>    Bu şekil bilinçli (paket dışından bir oturum kurulamaz demek değil —
>    `&sun.EV2Auth{…}` ile kurulabilir; kurulan şey yalnız **bayt üretir**, hiçbir
>    kapı açmaz). B2b bir `authenticated bool` eklemek yerine bu şekli korumayı
>    **yazılı bir karar** hâline getirmeli.
> 3'. **İki iddia daha ölçüme indirildi (2026-08-21, 3. denetim; kayıt yeter):**
>    **(S1)** `ev2.go` başlığının *"her tampon TAM nihai kapasitesinde ayrılır"*
>    cümlesi **fazla güçlüydü** — iki dosyadaki **on** `make([]byte, 0, …)` yerinin
>    **dokuzu** tam nihai uzunluğu ayırıyor, `ev2Pad` tek istisna ve bir **üst
>    sınır** ayırıyor (`len(in)+16`; 17 baytlık gövde → len 32, cap 33). İddianın
>    **iş gören yarısı** (*yeniden tahsis yok → öksüz kopya yok*) **onunda da**
>    geçerli, ve yazılmamış kuyruk baytları `make`'ten zaten sıfır → **sızıntı
>    yok**; yalnız *"tam"* kelimesi ölçüme indirildi.
>    **(S2)** `TestChangeKey_RejectsInvalidArguments`'ın iki numara vakası eski ve
>    yeni anahtar olarak **aynı** değeri geçiyordu, yani F3'ün no-op kapısı da
>    ateşlerdi ve iki alt test **numara kapısı silinse bile yeşil kalırdı** (bugün
>    geçmelerinin sebebi sıra, iddia değil). Girdi **ayırt edici** yapıldı: eski
>    anahtar artık farklı ve sıfırdan farklı, böylece ateşleyebilecek tek kapı
>    numara kapısı.
> 3. **`CmdCtr` monotonluğunu hiçbir Go katmanı garanti etmiyor** — `cmdCtr` her
>    çağrıda açık argüman, tek fren **çipin kendisi** (§9.1.2: yanlış sayaç =
>    yanlış MAC). Sayaç muhasebesi B2b'nin oturum nesnesinin işidir.

> **Kart düzeltmesi (2026-08-21, M8-05 FAZ B2b — komut katmanı).**
>
> **Sevk edilen (hâlâ taşımasız, hâlâ saf bayt üreten fonksiyonlar):**
> `internal/sun/apdu.go` (ISO 7816 sarmalayıcı · `ISO SELECT` · **üç çerçeveli**
> `GetVersion` + UID çıkarımı · `AuthenticateEV2First`'ün iki APDU çerçevesi ·
> `WriteData` komutu · durum sözcüğü ayrıştırma), `internal/sun/ndef.go` (mirror
> yer tutuculu NDEF URL şablonu + üç ofset) ve `internal/sun/filesettings.go`
> (`ChangeFileSettings` gövdesi + Tappa'nın normatif yapılandırması). HTTP · DB ·
> durumlu oturum · gerçek APDU taşıma **YOK** — onlar **B2c**. Yeni bağımlılık yok
> (`encoding/hex`, `fmt`, `strings` — stdlib). `internal/sun` kapsamı **%96,2**.
>
> ✅ **YAYIMLANMIŞ `ChangeFileSettings` VEKTÖRÜ ÜRETİLDİ — ve bu turun tek dış
> çapası odur.** Aynı kurucu AN12196 rev. 1.8 §6.9 tablo 19 (rev. 2.0 §5.9 tablo
> 18) **şifreli-PICC** yapılandırmasını birebir veriyor:
> `4000E0C1F121200000430000430000`. Böylece **alan sırası**, iki hak alanının
> **bayt bileşimi**, `FileOption`/`SDMOptions` bitleri, 3 baytlık **LSB-first**
> ofset kodlaması ve sarmalama **dışarıdan** çivilendi. Tappa'nın plain gövdesi
> (18 bayt, `4011E1C1F1E11D00003000003C00003C0000`) ondan yalnız **iki nibble
> değeri** ve **hangi ofsetlerin var olduğu** ile ayrılıyor.
> 🔴 **Ofsetler ÇAPRAZ ÖLÇÜLDÜ, kopyalanmadı:** test 32 ve 67'yi tablo 18'in kendi
> baytlarından **okumuyor**, tablo 17'nin `WriteData` gövdesindeki yer tutucu
> koşularını **buluyor** ve kurucudan tablo 18'i üretmesini istiyor — iki ayrı
> yayımlanmış dizi, aynı oturum. Bu, *"ofset dosyanın başından, NLEN dahil
> sayılır"* okumasını da kanıtlıyor (mesaja göreli okuma **iki eksik** olurdu).
>
> 🔴 **DERIVED, NOT TRANSCRIBED — kısa ve tam liste** (uzunu
> `internal/sun/filesettings.go` başlığında): *(1)* `SDMMetaRead = Eh`'in plain
> anlamı ve `UIDOffset`+`SDMReadCtrOffset`'i **var**, `PICCDataOffset`'i **yok**
> etmesi · *(2)* o iki ofsetin alan sırasındaki **yeri** · *(3)* NFC Forum URI RTD
> 1.0'ın `04h = https://` eşlemesi (üçüncü bir belge) · *(4)* NDEF dosyasının
> 256 baytlık **tavanı** (yalnız bir reddetme sınırı, tele giden bayt değil) ·
> *(5)* `GetVersion` üçüncü çerçevesinin **uzunluk alt sınırı**.
>
> ⚠️ **BU BLOK BURADA *"belgenin ayırt edemediği ÜÇ alan konumu"* DİYORDU VE
> İKİSİ YANLIŞTI — denetim turunda düzeltildi, aşağıdaki bloğa bakın.** Tablo 69
> `SDMOptions` bitlerini ve `SDMAccessRights` nibble'larını **isimle** veriyor;
> gerçekten ayırt edilemeyen **tek** çift `SDMMACInputOffset` ↔ `SDMMACOffset`'tir
> ve o **reddetmeyle** kapatıldı: kurucu **boş olmayan MAC girdisi taşıyan gövdeyi
> hiç kurmuyor** (ADR 0003 md. 2 zaten eşitlik istiyor).
>
> **Mutasyon kanıtı: 27 mutasyon, 25'i kırmızı.** Ofset MSB-first · `AccessRights`
> baytlarının ve `Read`/`Write` nibble'larının yer değiştirmesi ·
> `SDMAccessRights` baytlarının ve `MetaRead`/`FileRead` nibble'larının yer
> değiştirmesi · `RFU` nibble'ı · üç varlık koşulu · `UIDOffset`/`ReadCtrOffset`
> sırası · `FileOption` SDM biti · iki `SDMOptions` biti · `NLEN`'in LSB-first
> yazılması · ofsetlerin NLEN'siz sayılması · `WriteData` başlığında uzunluk ve
> ofsetin yer değiştirmesi · uzunluğun pad'li gövdeyi sayması · `GetVersion`'ın
> 7 baytlık üçüncü çerçeve kabul etmesi · UID'nin **birinci** çerçeveden alınması ·
> `ISO SELECT` P2'si · `NativeAPDU`'nun `Le`'yi düşürmesi · URI ön eki
> `https→http` · yük uzunluğunun ön ek baytını saymaması · MAC ofsetinin bir bayt
> kayması · `ChangeFileSettings` başlığından dosya numarasının düşmesi.
> **İki hayatta kalan** yukarıdaki ayırt-edilemez konumlardır ve **ikisi de
> tasarımla zararsızdır**, bir test boşluğu değil.
> 🔴 **VE BİR MUTASYON GERÇEK BİR BOŞLUK BULDU:** URI ön ekini `04h`'den `03h`'ye
> (`http://`) çeviren mutasyon **ilk turda hayatta kaldı**, çünkü test beklenen
> değeri **sabitin kendisinden** okuyordu — M2-08'in birebir şekli. Test artık
> belgeden gelen **04 harfini** yazıyor; mutasyon kırmızı.
>
> ✅ **ADR 0017 §6 MD. 13 KAPANDI: `Change` = `Write` = `ReadWrite` = anahtar
> `0x01`, `Read` = `Eh`.** Gerekçe ve durum-durum kurtarma sayımı ADR 0017 §6
> md. 13'te; iki *"karara bağlanmadı"* hücresi `deploy/README.md`'nin ayar
> tablosunda **dolduruldu**. 🔴 **ADR 0005 risk 8 DARALDI** (satır **eklenmedi**,
> sicil sekiz risk): fabrika anahtarı 0 ile **NDEF yazılamaz** ve **SDM
> kapatılamaz** → **oltalama vektörü kapandı**; kalan yarı **hizmet reddi + anahtar
> 0 kişiselleştirmesinin önden alınması** ve o da **sinyalsiz**.
>
> ⚠️ **B2a'nın BİR ÖLÇÜMÜNE İTİRAZ EDİLDİ VE DÜZELTİLDİ.** `ev2_kat_test.go`
> tablo 17'nin `800000`'ını *"ENCRYPTED length"* diye niteliyordu; ölçüldü:
> yayımlanmış `CmdData` **128**, şifreli alan **144**. Yani `0x80` ne 83 ne 144 —
> `len(CmdData)`, yani **yazılan düz metin**. Bu ayrım `EV2WriteDataCommand`'ın o
> alana ne koyacağını belirlediği için iş görüyor; testi artık üç sayıyı da
> doğruluyor.
>
> **Denetim turu (2026-08-21, bağımsız üçüncü göz — AN12196 rev 2.0 ve NT4H2421Gx
> rev 3.0 PDF'leri indirilerek, her *"belge diyor ki"* cümlesi `pdftotext`
> çıktısından okunarak). VERDICT: RED — iki bloklayan + altı bloklamayan; hepsi
> kapatıldı.**
>
> 🔴 **BLOKLAYAN 1 — *"ölçemedim"* dediğim iki vektör YAYIMLANMIŞ.** İkisi de
> `tags.uid`'i (**PRIMARY KEY**) üreten komutlardı, yani **sıfır dış çapayla**
> sevk ediliyorlardı, çapa **eldeyken**:
> - `ISO SELECT` — AN12196 rev 2.0 **§5.3 tablo 9 adım 10** tam C-APDU'yu basıyor:
>   `00A4040C07D276000085010100`, üretilen baytların **birebir aynısı**. Kod
>   *"this repository holds no vector for either"* diyordu.
>   → `TestAPDU_ISOSelectMatchesAN12196Table9`.
> - `GetVersion` üçüncü çerçevesi — **§5.5 tablo 12 adım 9**:
>   `04968CAA5C5E80CD65935D4021189100`, **14 veri baytı**, UID `04968CAA5C5E80`.
>   Veri sayfası **tablo 58 (s.60)** kalem kalem sayıyor → **14 veya 15**. Kod
>   *"this round could not measure that trailer's exact length"* diyordu; alt
>   sınır artık **transkript**, ve bir **üst sınır** da eklendi.
>   → `TestGetVersionKAT_ThirdFrameMatchesAN12196Table12`.
>   🔴 Ağırlaştırıcı: `commands_kat_test.go` §5.3'ü **SOURCES tablosunda
>   listeliyordu** ve hiçbir test kullanmıyordu; `an12196_kat_test.go`'nun eşleme
>   tablosunda ise §5.3 **hiç yoktu** → o tablonun **beşinci** yanlışlanması
>   (round 7 olarak kaydedildi; rev 1.8 yarısı **ölçülmedi diye BOŞ bırakıldı**,
>   tahmin edilmedi).
> **Sınıfı:** `https→http` mutasyonunun **tersi**. Orada test kendi sabitinden
> okuyordu (var olmayan bir hatayı gizliyordu); burada **var olan kanıt yok
> sayılıyordu**. Birincisi mutasyonda kendini gösterir, ikincisi **kimse bakmazsa
> hiç görünmez**.
>
> 🔴 **BLOKLAYAN 2 — *"çözülemiyor"* dediğim iki atama Tablo 69'da (s.66) YAZIYOR.**
> `SDMOptions`: bit 7 UID · 6 SDMReadCtr · 5 SDMReadCtrLimit · 4 SDMENCFileData ·
> 3-1 RFU · 0 ASCII. `SDMAccessRights`: 15-12 `SDMMetaRead` · 11-8 `SDMFileRead` ·
> 7-4 RFU · 3-0 `SDMCtrRet` — ve AN12196 T18 adım 7'nin **kendi metni** nibble'ları
> tel sırasında etiketliyor. **Baytlar zaten doğruydu; yanlış olan ETİKETTİ**, ve
> bedeli ölçülebilirdi: `ReadWrite↔Change` mutasyonu **hayatta kalmıştı**, çünkü
> hiçbir test o iki alanı **farklı** değerlerle kurmuyordu.
> → `TestFileSettings_NibblePositionsAreTheOnesTable69Names` (yedi nibble'ın yedisi
> de farklı; `23E1`/`F2E1` beklentileri **Tablo 7 + Tablo 69'dan türetilmiş**) ve
> mutasyon artık **KIRMIZI**. *"Zararsızlaştırma"* testi (`…AreHarmless`)
> **silindi** — öncülü yanlış cümleydi. `SDMCtrRet = 0x01` artık bir **KARAR**
> olarak yazılı (Tablo 9: `GetFileCounters`'ı kilitler; serbest bırakmak plakete
> dokunabilen herkese o kapının **kaç kez tap edildiğini** okuturdu).
> ✅ **Ve bir hayatta kalan GERÇEKTEN meşru:** `SDMMACInputOffset` ↔ `SDMMACOffset`
> — **T18 adım 7 o ikisini Tablo 69'un TERS sırasında listeliyor** ve örnekte ikisi
> de `430000`, yani hiçbir yayımlanmış bayt hakemlik edemiyor. Reddetme doğru çözüm.
>
> **Bloklamayanlar:**
> - **N1 — dört atıf yanlış yere işaret ediyordu** (baytlar doğru, işaret yanlış;
>   *"yanlış atıf, sonraki okuru doğrulayamayacağı bir sayfaya yollar"*):
>   `ISOSelectFile` §10.3.1 → **§10.9.1 tablo 84 (s.77)** · `WriteData` §10.8.2
>   tablo 74 → **tablo 81 (s.75)** · NDEF dosya numarası/boyutu §8.2.2 →
>   **§8.2.3 tablo 5 (s.10)** · dosya hakları aralığı tablo 69 → **tablo 6
>   (§8.2.3.3)** · tablo 69 sayfa aralığı **65–67** (varlık koşulları **s.67**).
> - **N2 — `ndefFileSize = 256` *"ölçülmemiş okuma"* diye etiketlenmişti**;
>   **tablo 5 (s.10)** onu veriyor ve §8.2.3.2 CC içeriğinde tekrarlıyor →
>   **transkript**. Bloklayan 2'nin aynısı, ters yönde.
> - **N3 — etiketlenmemiş türetme, ve belge onu yalanlıyor.** **Tablo 81:**
>   `WriteData`'nın veri alanı *"up to **248 byte** including secure messaging"*;
>   `apduMaxLc` 255'ti, yani **239 baytlık bir NDEF dosyası kabul ediliyordu** ve
>   çip `LENGTH_ERROR` verirdi — **oturumun ortasında**, adım 5'te. Artık
>   **birleştirilmiş alan üstünden** reddediliyor (`writeDataMaxDataField = 248`;
>   düz metin tavanı **223 bayt**). ⚠️ Ve daha ağırı: **§8.2.3.1** *"single frames
>   **up to 128 bytes** … are also tearing protected"* → 128'in üstündeki şablon
>   **tearing korumasını kaybeder**, oysa ADR 0017 §5.3'ün tamamı yarım yazma
>   üzerine ve **ayırt edemediği tek çift** *"hiç dokunulmadı"* ↔ *"yalnız adım 5
>   yazıldı"*. **KARAR (2026-08-21):** `BuildTapNDEF` 128 baytın üstünü **reddeder**
>   — host+path bütçesi **69 bayt**, bugünkü şablon 76 baytın tamamı 128'in altında,
>   ve Q08 host'u **henüz seçmedi**, yani kısıtı koymanın en ucuz anı.
> - **N4 — `SDMCtrRet`'in aralık kapısı yoktu** (üç kardeşinin hepsinde vardı).
>   Sonda: `0x07` kabul ediliyordu → çipte `PARAMETER_ERROR` (tablo 71) ve
>   **§9.1.10 gereği oturum ölür**, yani adım 5/6 koşmuşken adım 7 düşer. Kapı
>   eklendi (`0h..4h`, `Eh`, `Fh`).
> - **N5 — ADR 0005'in *"ölçülmedi"* satırı ölçüldü ve içinde KALICI bir madde
>   var.** `SetConfiguration` **§10.5.1** gereği **AppMasterKey** ister → fabrika
>   anahtarı 0 ile erişilebilir; **tablo 50**'nin `05h` seçeneği **LRP modunu geri
>   alınamaz** şekilde açıyor (*"cannot be disabled afterwards"*) ve LRP'de SDM MAC
>   **§9.3.8.2**'nin ayrı yoluyla hesaplanıyor → **doğrulayıcımızın asla
>   okuyamayacağı bir plaket**. ✅ Rahatlatıcı yarı: **Random ID plain UID mirror'ı
>   bozmuyor** (s.12). Sınıf değişmedi (hizmet reddi, `retire + replace`); ADR 0005
>   risk 8 güncellendi, **satır eklenmedi**, sayı **8**.
> - **N6 — üç bayat/yanlış cümle.** `ev2_kat_test.go`'nun *"md. 13 … does list"*'i
>   (ikizi `ev2.go`'da düzeltilmiş, bu **bırakılmıştı** — *"mekanizma taşındı, nesir
>   ayakta kaldı"*) · `an12196_kat_test.go`'nun round **6** notu round **5**'in
>   **üstüne** yazılmıştı ve *"diğer atıfların hepsi NT4H2421Gx'e"* cümlesi
>   **yanlıştı** (dört dosya AN12196 §5.3/§5.5/§5.6/§5.8.2/§5.9'a atıf yapıyor) ·
>   alt-test adı `…is_five_bytes_shorter_still` — gövde 18→**3**, yani **on beş**.
>
> **Mutasyon (denetim sonrası): 32 mutasyon, 31'i kırmızı.** İlk tur 27/25'ti;
> denetim beş mutasyon ekledi (dört yeni kapının her biri + `RFU↔SDMCtrRet`) ve
> **`ReadWrite↔Change`'i kapattı** — artık `TestFileSettings_NibblePositionsAreTheOnesTable69Names`
> tarafından kırmızıya çevriliyor. **Kalan tek hayatta kalan `MACInput↔MAC`** ve
> **meşruluğu T18'in ters sırasıyla** gerekçeli.
>
> **Güvenlik denetimi turu (2026-08-21, `tappa-security-auditor` — VERDICT: RED,
> bir bloklayan + üç bloklamayan). 🔴 KARAR DOĞRU ÇIKTI, SAYIM YANLIŞTI: kod
> değişmedi, METİN değişti.**
>
> Temiz bulunanlar (hepsi denetçinin kendi mutasyonuyla): §4.1–§4.6 ve §4.8 · §4.7
> sızıntı taraması (üç dosyadaki her biçim fiili yalnız uzunluk/indeks/nibble/
> ofset/anahtar numarası/durum sözcüğü taşıyor) · üç dosyada **sıfır `defer Zero`**
> (envanterin istediği hâl, iki yönde ölçüldü) · sabit-zaman envanteri ·
> `cmac.go`/`verify_mac.go` **dokunulmamış** (T64) · kararın dört nibble'ı da
> mutasyonla çivili · `go.mod` değişmemiş. Q08 bütçesi de ölçüldü:
> `tappa.mt/t` → 69 bayt, `tap.tappa.mt/t` → 73, 35 karakterlik host → 96; hepsi
> 128'in altında ve 70 bayt üstü **hata döndürüyor, sessiz kırpma yok**.
>
> 🔴 **BLOKLAYAN — yazma yetkisi, "yapısal olarak sızar" denen anahtara bağlandı.**
> `Write = ReadWrite = Change = 0x01` ve **`0x01`, ADR 0005 risk 7'nin her encode ve
> her kurtarma oturumunda dökümü görene sızdığını söylediği anahtardır**. Zincirin
> her halkası bu turun kendi kodunda: risk 7'nin kümesi `K_0x01`'i öğrenir →
> `AuthenticateEV2First(0x01)` mümkün (§5.3 sondası zaten onu kullanıyor) → o
> oturumda `WriteData` **ve** `ChangeFileSettings` açık → **NDEF repointlenebilir =
> oltalama**. Ve risk 7'nin kendi azaltma cümlesi (*"§5'in diğer üç kanıtı
> bağlamaya devam eder"*) o dalda **geçersiz**: repointlenen plaketin tap'i **bize
> hiç ulaşmaz**, yani çelişecek bir çerez/IP/GPS doğmaz.
> **Karar korundu** — `0x00` halka açık (daha kötü), teslim `Eh` serbest (daha
> kötü), `Fh` Q08 ve §5.3 gerekçesiyle zaten reddedilmiş. **Düzeltilen SAYIM:**
> risk 7'nin sonuç sütunu ve gövdesi artık *"sahte SUN **VEYA** NDEF repointleme
> (oltalama, sinyalsiz)"* diyor ve azaltma cümlesi o dalda geçersiz diye yazılı ·
> risk 8'in satırı, daralma tablosu ve ADR 0017 §6 md. 13 **iki niteleyici** aldı:
> **KİM** (yalnız fabrika anahtarı 0'ı olan taraf, risk 7'nin kümesi değil) ve
> **NE ZAMAN** (yalnız **adım 7 tamamlandıktan sonra**; adım 5–7 arasında kesilen
> çipte `Write = ReadWrite = Eh`, yani **serbest**). **Satır eklenmedi, sayı 8.**
>
> **Bloklamayanlar:**
> - **N7 — `ParseGetVersion` UID İÇERİĞİNİ hiç doğrulamıyordu.** Denetçi
>   `00000000000000`, `FFFFFFFFFFFFFF` ve **seed'in gerçek plaket UID'sini** verdi;
>   üçü de kabul ediliyordu. Belgeden **ölçülerek** iki kapı eklendi: veri sayfası
>   **§8.1 (s.8)** *"the first byte of the double size UID is **fixed to 04h**"* ve
>   🔴 **Tablo 58 (s.60)** UID alanı için *"**All zero** — if configured for
>   RandomID"* — yani sıfır UID **dejenere görünüm değil, belgelenmiş bir durum**:
>   çip UID'ini **söylemiyor**. Onu `tags.uid`'e (**PRIMARY KEY**) yazmak ilk
>   plakette işgal, sonrakilerde çakışma olurdu.
>   🔴 **Ve dürüst sayım: bu saldırıyı KAPATMAZ.** Yalancı bir röle **geçerli
>   görünen bir kurban UID'si** döndürebilir ve `GetVersion` yanıtının hiçbir yerinde
>   kimlik doğrulama yoktur — test bu vakayı **kabul ederek** ve sebebini yazarak
>   tutuyor. **Gerçek çare** ADR 0017 §6 md. 12'ye **dördüncü sonuç** olarak yazıldı:
>   adım 6'dan sonra `K_0x01` bizimken UID'yi **`GetCardUID` ile o oturumda yeniden
>   oku** (`CommMode.Full`, yanıt MAC'i röle tarafından üretilemez) — **yalanı
>   ⚠️ *(Bu iki yarı da sonradan çürütüldü: yerleşim FAZ B2c-1'de **adım 4b**'ye
>   taşındı, MAC iddiası geri çekildi. Bu satır **tarihsel bir B2b kaydıdır** ve
>   olduğu gibi bırakılıyor; güncel hâl ADR 0017 §6 md. 12'dedir.)*
>   TESPİT eder, satırı önlemez**. Komut kurucusu sevk edildi (`GetCardUIDCommand`).
> - **N8 — §5.3'ün 1 numaralı sondası yazılmamıştı** ve açık listede de yoktu.
>   Sevk edildi: `GetFileSettingsCommand` + `ParseFileSettings`. ⚠️ *"Kimlik
>   doğrulamadan önce koşar"* artık **çıkarım değil ölçüm**: Tablo 22 komutu
>   `CommMode.MAC` diye listeliyor (aynı etiketi `GetVersion`'a da veriyor, ve onun
>   kendi bölümü *"freely accessible without secure messaging"* diyor), AN12196 ise
>   soruyu **yaparak** kapatıyor — dizide adım **4**, kimlik doğrulaması olan adım
>   **6**'dan önce. `Change = 0x01` sondayı **bozmuyor** (Tablo 9 `Change`'i yalnız
>   `ChangeFileSettings`'e bağlar).
>   🔴 **Ve ayrıştırıcı için DIŞ ÇAPA VAR:** AN12196 §5.4 Tablo 11'in alan listesi
>   §5.9 Tablo 18'in `CmdData`'sının **aynısıdır**, yani yanıt → ayrıştır →
>   yeniden kodla turu **yayımlanmış bir dizeye** iniyor. ⚠️ Belgenin **basılı
>   R-APDU dizesi kullanılmadı** ve sebebi ölçüldü: `004300E0...9100` **18 bayt** ve
>   `FileOption = 43h` gösteriyor, oysa kendi Tablo 11'i **19 bayt** ve `40h`
>   diyor — dördüncü belge iç çelişkisi. (Beşincisi: Tablo 11 ofsetlerini
>   `UIDOffset`/`SDMReadCtrOffset` diye **etiketliyor**, oysa kendi
>   `SDMMetaRead = 2h` değeri altında Tablo 73'ün varlık koşulları o ikisinin
>   **yok** olduğunu söylüyor.)
> - **N9 — daralma tablosunun durum niteleyicisi yoktu**; eklendi (yukarıda).
>
> **Denetçinin doğrulayamadıkları — ÜÇÜ DE PDF'ten ölçüldü (bu turda indirildi):**
> **(1)** `SetConfiguration` *"requires an authentication with the **AppMasterKey**"*
> ✅ (§10.5.1) · LRP ✅ ama **adı düzeltildi**: seçenek `05h` **"Capability data"**,
> LRP onun içindeki `PDCap2.1` **bit 1**'dir, *"This change is permanent"* ✅ ·
> **Tablo 81 = 248 "including secure messaging"** ✅ · **§8.2.3.1 = 128** ✅.
> **(2)** 🔴 **Tablo 50 alıntısı KISMİYDİ ve öyle olduğunu söylemiyordu** — üç
> değil **beş** seçenek (`00h`, `04h`, `05h`, `0Ah`, `0Bh`); tamamı ADR 0005 risk
> 8'e yazıldı. Ve **komut tablosu (Tablo 22) tarandı**: anahtar 0 ile erişilip
> **kalıcı** etki bırakan küme = `SetConfiguration` (LRP kalıcı) + `ChangeKey`
> (anahtar 0, 2–4; eskileri halka açık sıfır). 🔴 **Tarama md. 13'ün hiç
> kapsamadığı bir yol buldu:** Tablo 8'e göre dosya `01h` (**Capability Container**)
> teslimde `Write = ReadWrite = 0h`, dosya `03h` `3h` → anahtar `0x00`/`0x02`–`0x04`
> fabrika sıfırında kaldıkça **ikisi de herkese yazılabilir**, ve CC'yi `E105h`'e
> yönlendirip oraya URL yazmak **dosya `02h`'ye hiç dokunmadan** oltalama üretir.
> Kapatan şey md. 13 değil **md. 5**'tir; ADR 0005 risk 8'e ve ADR 0017 md. 13'e
> yazıldı.
> **(3)** **128 baytlık tearing sınırı YAZILAN baytları ölçüyor**, mühürlenmiş
> çerçeveyi değil → **kapı doğru kalibre**. Kanıt belgenin kendi karşıtlığı:
> mühürlenmiş boyutu kastettiğinde **söylüyor** (Tablo 81 *"including secure
> messaging"*), §8.2.3.1'de böyle bir ibare yok ve cümle `ISOUpdateBinary`'yi de
> anıyor — o `CommMode.Plain`, yani orada yazılan bayt = çerçeve baytı.
>
> **Ayrıca bu turda belgeden doğrulanan üç şey daha:** Tablo 7 erişim haklarını
> **bit 15..12 Read · 11..8 Write · 7..4 ReadWrite · 3..0 Change** diye veriyor ve
> teslim değeri `E0EE` (Tablo 8 ile birebir) → **bayt bileşimi artık iki ayrı
> yerden çivili** · Tablo 73 varlık koşullarının **beşini de** komut yönündekiyle
> aynı sırada tekrarlıyor, **`SDMMACInputOffset` önce** → AN12196'nın ters
> etiketlemesi bir **hata**, ayırt edilemezlik değil · Tablo 58 üçüncü çerçeveyi
> UID(7)+BatchNo(4)+1+1+1[+1] diye sayıyor → **14 veya 15**, kodun aralığıyla aynı.
>
> **Üçüncü denetim (2026-08-21 — VERDICT: ONAY, bloklayan yok).** Denetçi iki
> PDF'i **md5'leriyle** doğruladı (`8c9a4453…` AN12196 rev 2.0, `dcf5319c…`
> NT4H2421Gx rev 3.0 — bu turda aynı hash'ler yeniden ölçüldü) ve her iddiayı
> kendi ölçtü; hepsi tuttu. Kapatılan **beş** madde ve hepsi **güvenlik
> belgelerindeki iddiaları düzeltiyor**, kod değil:
> - 🔴 **Anahtar-0 ile erişilebilen KALICI küme eksikti.** `ChangeFileSettings`
>   ile dosya `01h`/`03h`'nin herhangi bir hakkını **`Fh`** (tablo 6: *"no
>   access"*) yapmak **geri alınamaz**: geri açacak komut yine
>   `ChangeFileSettings`'tir (tablo 9) ve veri sayfasında **format/fabrika
>   sıfırlama komutu yoktur** — tablo 22 taraması `FormatPICC|CreateApplication|
>   factory reset|restore.*default` için **sıfır eşleşme** (bu turda yeniden
>   koşuldu). Sonuçları: CC'yi repointledikten **sonra dondurmak** =
>   **düzeltilemez oltalama plaketi**; boş çipte `02h`'nin `Change`'ini `Fh`
>   yapmak = **adım 7'miz kalıcı olarak başarısız** (§5.3 sondası 1 bunu görür).
> - 🔴 **CC yolunun yarısı TÜRETME DEĞİL, YAYIMLANMIŞ ÖRNEK** — etiket fazla
>   temkinliydi. AN12196 **§5.14**: *"By default, CC file has FileAR.ReadWrite set
>   to 00. **Therefore Authentication with Key0 needs to be done**."* ve **§5.15
>   tablo 24** anahtar 0 altında CC'ye yazan tam C-APDU'yu basıyor — yazdığı şey
>   `E105h`'i adlandıran `Proprietary-File_Ctrl_TLV`. Türetilmiş kalan tek parça
>   telefonun CC'deki dosya kimliğini izlemesi. **İki ADR de artık AN12196'ya atıf
>   veriyor** (önceden hiç vermiyorlardı).
> - **Risk 8'in CC cümlesi bir adımı atlıyordu:** tablo 8 dosya `03h`'ye
>   `Read = 2h` veriyor, yani kimlik doğrulamamış telefon `E105h`'i **okuyamaz**;
>   saldırganın **ayrıca** `03h`'nin `Read`'ini `Eh`'ye taşıması gerekiyor
>   (mümkün: `Change = 0h`). Sonuç değişmiyor, ama cümle saldırıyı olduğundan
>   **kolay** gösteriyordu — sapma yanlış yöndeydi. Üç adım artık yazılı.
> - **`filesettings.go`'nun "19"u yanlış belgeye bağlanmıştı.** Tablo 11'in kendi
>   değer sütunu **16** veri baytı veriyor, yani basılı dizeyle **aynı**; **19**,
>   Tablo 11'in `SDMAccessRights = F121` değerine **NT4H2421Gx tablo 73'ün**
>   varlık koşulları uygulanınca çıkıyor (`7+3+3+3+3`). Çelişki her iki okumada da
>   gerçek ve kod doğru; yanlış olan **aritmetiğin kaynağıydı**.
> - **Bir "kendi şüphem" ölçüme indirildi.** *"Basılı dize doğruysa ayrıştırıcım
>   da yanlış"* — **değil**: ayrıştırıcı **NT4H2421Gx şekil 23 + tablo 73**'ü
>   uyguluyor, ve çipin yanıt biçimi için normatif olan onlar; AN12196'nın bu
>   konuda yetkisi yok. Yanlış olabilecek şey **vektör seçimi**, **bölme değil**.
>
> **Denetçinin fazladan yaptığı ve tutan üç ölçüm:** `ndefPrefixLen = 7` ve mutlak
> ofsetler (T17 → T18 `200000`/`430000`) · 248/223 aritmetiği (T17 adım 15
> `Lc = 0x9F = 7 + 144 + 8`) · **18 baytlık gövdenin sekiz alanının sekizi**.
>
> 🔴 **BU GÖREVİN ÖĞRETTİĞİ — PDF itirafının ÖTESİNDE.** Üç denetimin **üçü de**
> aynı sınıfı buldu ve sınıf *"yanlış bayt"* değildi: **fazla geniş bir cümle**.
> Sırasıyla *"vektör yok"* (vardı) · *"ayırt edilemez"* (Tablo 69 isimle veriyor) ·
> *"oltalama kapandı"* (yalnız bir saldırgan kümesine karşı) · *"ölçülmedi"*
> (ölçülebilirdi). **Hiçbiri testle yakalanmaz**, çünkü hiçbiri koda dair bir
> iddia değil — **kanıta dair** bir iddia. Mutasyon bir baytın korunduğunu
> gösterir; bir cümlenin **hak ettiğinden fazlasını söylediğini** göstermez.
> Ders operasyonel: **bir "yapamaz/yoktur/kapandı" cümlesi yazarken niteleyicisini
> aynı anda yaz** — *kime karşı, hangi durumda, hangi ölçümle* — çünkü niteleyici
> sonradan eklenmiyor, **denetim tarafından ekletiliyor**. Bu turda kararların
> hiçbiri değişmedi; değişen **on iki cümle** oldu.
>
> 🔴 **AÇIK KALAN — B2c'nin işi:** ADR 0017 §6'nın md. 5 (anahtar 0'ın şeması) ·
> md. 7 (oturum TTL/süpürücü/`Zero` garantisi) · md. 8 (`audit_log`) · md. 10
> (yetkilendirme kapısı) · md. 12 (UID uzayı), artı B2a'nın F4 devri (`crypto/rand`
> ile `RndA`, `CmdCtr` muhasebesi, `EV2Auth`'un şeklinin yazılı kararı) ve
> **hâlâ hiçbir çip encode edilmedi** (§6 md. 1).

> **Kart düzeltmesi (2026-08-21, M8-05 FAZ B2c-1 sırasında).**
>
> **Sevk edilen: `internal/encode` — ürünün ilk DURUMLU encode kodu.**
> `session.go` (bellek içi oturum deposu: keyring · TTL · süpürücü · eşzamanlılık
> sınırları · tek çıkış) ve `driver.go` (§5.1'in **on alışverişlik** durum makinesi).
> 🔴 **HTTP YOK · DB YOK · sqlc YOK · migration YOK**; `db/queries/` değişmedi.
> Kalıcılık, sarmalama ve saat **tüketici tarafında tanımlanmış üç arayüzle**
> (`Rows`, `Wrapper`, `Clock`) enjekte ediliyor. Yeni bağımlılık **yok**
> (`context`, `crypto/rand`, `crypto/aes`, `encoding/hex`, `errors`, `fmt`, `sync`,
> `time`, `bytes` — hepsi stdlib). Kapsam: `internal/encode` **%92,6**,
> `internal/sun` **%96,7** (düşmedi). **127 test vakası.**
> ⚠️ **Kapsam sayısı DETERMİNİSTİK DEĞİL** (5. denetim ölçtü): beş ardışık koşu
> **93,1 / 92,6 / 92,6 / 92,6 / 92,6** verdi (kendi ölçümüm). Raporlanan sayı **mod**, sabit değil —
> sebebi `Close`'un zaman aşımı dalındaki `if stuck == 0` gibi yarış-bağımlı
> satırların bazı koşularda hiç çalışmaması.
> **Mutasyon: 76 mutasyon, 67'si kırmızı, 9 hayatta kalan** — dokuzu da gerekçeli ve
> ikisinin **meşruluk ön koşulu ayrıca çivili** (aşağıda).
>
> ✅ **ADR 0017 §6 md. 7 KAPANDI — beş maddenin beşi de karara bağlandı ve gerekçe
> KODUN İÇİNDE:**
> 1. **TTL = 90 sn**, ve **iki taraftan da bağlı** (`TestSession_TheTTLCoversTheRoundAndNotMuchMore`).
>    Taban `len(roundSteps) × exchangeBudget` = 10 × 5 sn — **tabloyu sayarak
>    türetiliyor**, tekrar yazılmıyor. Tavan **tabanın iki katı**, ve tavanın
>    gerekçesi ölçüm: veri sayfası s. 28, alan düşünce çipin kimlik doğrulama
>    durumu *"immediately lost"* → sunucu oturumunun çipinkinden uzun yaşaması
>    **hiçbir şey satın almaz**, yalnız anahtar tutar. ⚠️ `exchangeBudget = 5 sn`
>    bir **bütçedir, ölçüm değil**, ve öyle etiketli: §6 md. 2 röle gecikmesinin
>    **ölçülmediğini** söylüyor, elimizdeki tek sayı `deploy/README`'nin ~150 ms'lik
>    kaba hesabı. FAZ B3 silikonda ölçtüğünde değişecek sabit budur.
> 2. **İKİSİ DE süpürür, ve ikisi de gerekli.** Tembel süpürme (`checkout`) **terk
>    edilmiş** oturuma **hiç ulaşmaz** — tanımı gereği kimse onu bir daha
>    aramıyor; süpürücü **goroutine** ise tick'ler arasında bir pencere bırakır.
>    `TestStore_TheSweeperIsAGoroutineAndItRuns` goroutine'i **hiçbir çağrı
>    yapmadan** kanıtlıyor (yalnız tick), pozitif kontrolüyle birlikte.
> 3. 🔴 **GARANTİNİN MEKANİK YÜZÜ ÜÇ PARÇA — B2a mekanizmayı vermişti, bu tur
>    garantiyi veriyor:** *(a)* `sun.Zero` **yalnız keyring metotlarında** çağrılır
>    (`TestSession_OnlyTheKeyringWipes`, go/ast) · *(b)* `zeroAll`'ın **tam bir**
>    çağıranı vardır, `Store.retireLocked`
>    (`TestSession_TheRingIsWipedFromExactlyOnePlace`) · *(c)* **hiçbir ihraç
>    edilmiş API bir `*Session` alıp vermez** (`TestSession_NoExportedAPIHandsOutASession`),
>    yani çağıran bir oturumu tutamaz, düşüremez. ⚠️ Üçü **varlığı ve tekliği**
>    kanıtlar, **kaydolmamayı** değil — o boşluk `TestStore_TheDrivenExitPathsWipeEveryKeyBuffer`
>    ile **davranışsal** kapatıldı: test **çağıranın kendi dilim başlıklarını**
>    tutuyor ve **yedi** çıkış yolunun (başarı · hata · TTL süpürmesi · tembel
>    süpürme · iptal · kapanış · abort) her birinde baytların sıfır olduğunu ölçüyor.
> 4. **Eşzamanlılık: plaket başına 1 · aktör başına 3 · depo geneli 64**, üçü de
>    gerekçesiyle. Plaket başına **red, tahliye değil** (tahliye eden taraf canlı
>    bir turu bir plaket bedeliyle öldürebilirdi); red bir **gecikme**, kayıp değil,
>    ve pozitif kontrolü var.
> 5. **Envanter TEK YERDE, `keyInventory`:** `KSesAuthENC` · `KSesAuthMAC` ·
>    `RndA` · `RndB` · `K_SDMFileRead` · **`K_AppMaster`** — sonuncusu **bugün hiç
>    doldurulmuyor** (md. 5 bloke) ama **slot açık**, yani şema kararı geldiğinde
>    silme/çıkış yolu **zaten kapsıyor**. `TI` ve `CmdCtr`'ın **neden listede
>    olmadığı** aynı yerde yazılı; oturum kimliğinin **silinemez bir Go dizesi**
>    olduğu **sayılmış limit** olarak duruyor.
>
> ✅ **§5.1 SIRASI TABLONUN KENDİSİ.** `roundSteps` on satır; hiçbir dal bir adımı
> sırasız üretemez. **6 ↔ 7** üç yönden çivili (tablo indeksi · tele çıkan INS
> dizisi · makinenin **hiçbir durumunda** adım 7'nin sıradaki komut olmaması), ve
> mutasyonla kırmızı. ✅ **§5.2 SIRASI da:** satır, `GetVersion`'ın **üçüncü
> çerçevesinin** accept'inde — UID'nin var olduğu ilk an — ve `AuthenticateEV2First`
> **çıkmadan önce** yazılıyor; `recordingRows` porta yapılan çağrılarla tele çıkan
> APDU'ları **tek bir sıralı günlükte** kaydediyor, mutasyon (satırı adım 4'ten
> sonraya almak) **dört testi** kırmızıya çeviriyor.
>
> ✅ **`RndA` DİSİPLİNİ YAPISAL, "dikkat ediyoruz" değil.** `crypto/rand`'dan
> üretiliyor, hata kontrollü, **keyring'e giriyor** ve `take` slotu **consumed**
> işaretliyor → **ikinci bir okuma ifade edilemez**. Tazelik **telden** ölçülüyor:
> çip Part 2 kriptogramından `RndA`'yı geri çözüyor, 16 turun 16'sı **ayrık**.
>
> 🔴 **VE BU TUR ADR'NİN KENDİ BİR CÜMLESİNİ ÖLÇEREK ÇÜRÜTTÜ — md. 12'nin dördüncü
> sonucu SEVK EDİLDİ, ama iddiası daraltıldı.** ADR *"`GetCardUID` `CommMode.Full`,
> yani yanıt MAC'i röle tarafından **üretilemez**"* diyordu. Ölçüldü: o oturum
> **anahtar 0** ile kurulur ve boş çipte anahtar 0 **halka açıktır**, yani ADR 0005
> **risk 7**'nin kümesi oturum anahtarlarını türetip **o MAC'i de üretir**; kontrolü
> taze bir `0x01` oturumuna taşımak da kurtarmaz (aynı döküm adım 6'nın `ChangeKey`
> gövdesini taşıyor). **Hak edilen dar cümle:** kapı bir **dizeyi düzenleyerek**
> yalan söyleyen röleyi yakalar, **EV2 güvenli mesajlaşmasını uygulayan** §2.2
> rölesini yakalamaz. Kod, test ve ADR artık **tam olarak bunu** söylüyor;
> `internal/sun/apdu.go`'daki ikiz cümle de daraltıldı. Kapı **kaldırılmadı** —
> ucuz, sevk edildi, ve tespit ettiği yalan *"iptal et ve temizle"*ye çevriliyor;
> temizlik yolu (`retire + replace` + çipin hurdaya ayrılması, **bugün elle,
> `tappa_owner` ile**) `RelayMismatchError`'da **adıyla** yazılı, ve test satırın
> **silinmediğini** de iddia ediyor.
>
> ✅ **İKİ TASARIM KARARI YAZILDI:**
> - **`EV2Auth` DEĞİŞMEDİ** (`internal/sun/ev2.go`'ya bu turda dokunulmadı) ve
>   `authenticated bool` **eklenmedi**. Karar: **kimlik doğrulama durumu oturumun
>   adım indeksinde yaşar** — `Session`'ın **hiçbir ihraç edilmiş alanı yok** ve
>   paket dışından kurulamaz/okunamaz. `TestSession_TheEV2AuthShapeIsTheOneThisPackageInventories`
>   `EV2Auth`'un **beş alanını** reflect ile çiviliyor: altıncı bir alan (bayrak ya
>   da yeni sır) bu kararı **kırmızı** yapar.
> - **`CmdCtr` ARTIK BİR GO KATMANINDA.** `Session.useCtr()` sayacı verir ve
>   ilerletir; **setter yok**, çağıran değer sağlamaz. Kanıt dolaylı ve asıl güçlü
>   olan o: sahte çip **her komut MAC'ini kendi sayacıyla** doğruluyor, yani
>   atlanan/tekrarlanan/önden artırılmış bir sayaç **INTEGRITY_ERROR** veriyor
>   (mutasyon M12 → dört test kırmızı). `FFFFh` sınırı ayrıca reddediliyor.
>
> **Sahte çip (`chip_test.go`) NE KANITLAR, NE KANITLAMAZ — kendi başlığında yazılı.**
> Kanıtlar: sayaç muhasebesi, `TI`, alan bölünmesi, sıra (komut MAC'lerini
> **bağımsız** bir RFC 4493 CMAC'iyle doğruluyor). **Kanıtlamaz:** silikon hakkında
> hiçbir şey (§6 md. 1 duruyor) · KDF hakkında hiçbir şey (oturum anahtarlarını
> `sun.EV2AuthPart2`'den **ödünç alıyor**; o zaten AN12196 vektörleriyle **dışarıdan**
> çivili) · `CRC32NK` hakkında hiçbir şey (bilerek yeniden hesaplamıyor).
>
> **Mutasyon kanıtı: 76 mutasyon, 67'si kırmızı, 9 hayatta kalan.**
> Kapsanan kararlar: `zeroAll`'ın silinmesi · `take`'in consumed'ı bırakması
> (RndA tekrarı) · adım 6/7'nin ters çevrilmesi · satırın adım 4'ten sonraya
> alınması · `GetCardUID` karşılaştırmasının **ve** uzunluk kapısının kaldırılması ·
> süpürücünün iki ayrı yolla kapatılması · üç eşzamanlılık sınırının kaldırılması ·
> sayacın artmaması, `FFFFh`'de sarması ve yanıtın **yanlış sayaçla** doğrulanması ·
> durum sözcüğünün denetlenmemesi · tembel süpürmenin kaldırılması · `add`'in
> reddettiğini silmemesi · iptalin yok sayılması · hatada oturumun emekli
> edilmemesi · işaretleyici hatasında `Done`'ın düşürülmesi · `ISO SELECT` ve
> mühürlü-onay gövde kapıları · `Close`'un oturumları bırakması · satır kapısının
> düşürülmesi · **plaket anahtarının sabitlenmesi** · **oturum kimliğinin sayaca
> çevrilmesi** · `sweepDivisor`, `DefaultMaxLive`, `DefaultMaxPerActor`,
> `exchangeBudget`, `keyVersion`'ın oynatılması · **panik çıkış yolunun
> kaldırılması** · **yanlış AAD ile sarmalama** · **satıra başka bir anahtarın
> sarmalanması** · **`ChangeKey`'in anahtar 0'a gitmesi** · **şablonun yanlış
> dosyaya yazılması** · `ISO SELECT`'in atlanması.
>
> 🔴 **HAYATTA KALANLAR — HER BİRİ AYRI AYRI YAZILI.** ⚠️ Bu bir **örneklem**
> ifadesidir, bir kod özelliği değil: *"tek ve meşru"* gibi kesin tekil bir cümle
> **kurulmuyor**, çünkü bir sonraki denetçinin kuracağı başka bir mutasyon kümesi
> başka hayatta kalanlar bulabilir — nitekim 2. denetim tam olarak bunu yaptı.
> 1. **`ParseGetVersion`'a çerçeve 1 ile 2'nin ters verilmesi.** Meşru, çünkü
>    ölçüldü: UID **üçüncü** çerçeveden okunuyor, ilk ikisi yalnız **uzunluk**
>    kapısından geçiyor, ve bu paket `Version`'dan **yalnız `UID`** okuyor. Meşruluk
>    **koşullu** olduğu için koşul çivilendi:
>    `TestSession_TheDriverReadsOnlyTheUIDOutOfGetVersion` (go/ast) bu paket
>    `Hardware`/`Software`/`Production` okumaya başladığı gün kırmızıya döner.
> 2. **Kimlik doğrulama sonrası `s.cmdCtr = 0` atamasının silinmesi.** Meşru: alan
>    `Begin`'in **taze ayırdığı** bir `Session`'da zaten sıfır; atama yalnız bir
>    oturumun **yeniden kullanılmasına** karşı savunma. Ön koşul çivilendi:
>    `TestSession_ASessionIsOnlyEverConstructedOnce` (go/ast) `Session` bileşik
>    değişmezinin **tam bir** üretim fonksiyonunda (yalnız `Begin`) kurulduğunu
>    ölçüyor; havuzlama geldiği gün kırmızı.
> 3. **Zaman aşımı dalının `retireIdleLocked()` yerine `len(st.live)` sayması**
>    (M63, bu turda eklendi). Meşru: değişiklik **yapısal bir sertleştirme**, gözlenen
>    bir kusurun düzeltmesi değil — pencereyi ne denetçi ne ben üretebildik (**400
>    denemede 0** yanlış alarm), dolayısıyla hiçbir davranışsal test ikisini ayırt
>    edemez. Sayının taze bir sweep'ten türetilmesi pencereyi **şansa değil yapıya**
>    bağlıyor.
> 4. **Sahte çipin `ChangeKey` gövde-biçimi ölçütünün oturuma çevrilmesi** (M64, bu
>    turda eklendi). Meşru **ve ölçülmüş**: Tablo 65'in ön koşulu uygulandığı için
>    `ChangeKey` yalnız anahtar-0 oturumunda yasaldır, dolayısıyla `keyNo == AuthKey`
>    ile `keyNo == 0` **her girdide aynı dalı seçer** — hiçbir davranışsal test
>    ikisini ayırt edemez. ADR 0017 §5.1 bunu zaten söylüyor: *"ikisi aynı kapıya
>    çıkar — ama kod **numaraya** bakmalıdır, oturuma değil."* Kod numaraya bakıyor;
>    ayrım adım 8 ya da probe-2 akışı geldiğinde **gerçek** olacak.
> 5. 🔴 **Anahtar baytlarının başka bir tipin MAP ANAHTARINA sızması** (M76) ve
>    🔴 **bir CLOSURE YAKALAMASINA sızması** (M77). **Meşru hayatta kalan DEĞİL —
>    BUNLAR SAYILMIŞ AÇIĞIN KENDİSİ** (md. 15). Hiçbir kapı görmüyor; map anahtarı
>    ayrıca **kalıcı olarak** silinemez (Go dizesi). Mutasyon tablosunda **kalıcı
>    olarak** tutuluyorlar ki açık **görünür** kalsın, bir gün mekanikleşirse
>    **kırmızıya dönsünler**.
> 6. **`finishLocked`'ın switch'inde `case done:`'ın yerini değiştirmek** (M78).
>    Meşru **ve ölçüldü**: o switch'in **hiçbir sırası yük taşımıyor** — tek çağrı
>    yeri var, `done` doğruyken dönüşü `Step` hiç okumuyor, ve üç dal da **aynı tek
>    ifadeyi** çalıştırıyor. Bu mutasyonun hayatta kalması, bloklayan 2'nin
>    düzeltmesinin **dayandığı ölçümdür**.
> 7. **`finishLocked`'ın `signalDrainLocked()` çağrısının silinmesi** (M79). Meşru:
>    `Close` yalnız **meşgul** oturumları bekliyor ve hepsini `expireLocked` ediyor,
>    dolayısıyla sahibinin `finishLocked`'ı **daima** `retireLocked`'a düşüyor ve
>    sinyali **o** veriyor — buradaki çağrı **savunmacı fazlalıktır**. ⚠️ Kolun
>    kendisi yük taşıyor: `finishLocked`'ın **deadline dalını silmek KIRMIZI** (M80,
>    kontrol).
> 8. **`retireLocked`'da `s.auth = nil`'in silinmesi.** Meşru ve **çivilenmiyor**:
>    `auth.KeyENC`/`KeyMAC` **halkada kayıtlı** ve `zeroAll` **aynı arka dizileri**
>    siliyor; işaretçiyi `nil`'lemek bir **takma adı** kaldırıyor, bir sırrı değil.
>    Geriye kalan `TI` ve yetenek baytları §4.7 anlamında sır değil.
>
> 🔴 **İLK GEÇİŞLERDE ÜÇ MUTASYON HAYATTA KALDI VE ÜÇÜ DE GERÇEK ZAYIFLIKTI:**
> *(i)* `GetCardUID` **uzunluk** kapısını silmek yeşil kalıyordu (`bytes.Equal` kısa
> çerçevede zaten `false`) — ama o zaman kırık bir aktarım **`RelayMismatchError`**
> olarak raporlanır, yani operatör bir **taşıma arızası** için satırı emekli edip
> çipi **hurdaya** yollar; test artık hata **sınıfını** iddia ediyor.
> *(ii)* **Satır kapısı** testi *"bir hata döndü"* diyordu, oysa satırsız bir
> oturumun oturum anahtarı da yok — kod ve mutantı **aynı şekilde** düşüyordu; test
> artık **hangi kapının** ateşlediğini iddia ediyor. *(iii)* Şablonun **dosya 03h**'ye
> yazılması yeşil kalıyordu ve sebebi **sahte çipin kendi zayıflığıydı**: tek bir
> `ndef` alanı tutuyor, dosya numarasını yok sayıyordu. Çip artık **dosya başına**
> saklıyor ve tam tur testi şablonun **02h'ye ve yalnız 02h'ye** indiğini ölçüyor.
>
> 🔴 **AÇIK KALAN — B2c-2'nin işi, ve hiçbiri bu turda kapanmadı:** ADR 0017 §6
> md. 5 (anahtar 0'ın şeması → **§5.1 adım 8 SEVK EDİLMEDİ**, ve
> `TestDriver_NoChangeKeyIsEverEmittedForApplicationKeyZero` onu kapalı tutuyor;
> bedeli **ADR 0005 risk 8** ve *"anahtar 0 fabrikadayken plaket duvara çıkamaz"*
> çizgisi aynen duruyor) · md. 8 (`audit_log` — olay adı/aktör/tenant üçü de
> kararsız, bu yüzden **hiçbiri uydurulmadı**) · md. 10 (yetkilendirme kapısı —
> `Begin`'in `actor`'ı bir **maruziyet sınırıdır, yetki değil**, ve kodda öyle
> yazılı) · md. 11 (Q08 host'u) · §5.3'ün üç sondası (komut kurucuları `internal/sun`'da
> **var**, sürücüsü yok — ayrı bir akış) · ve **hâlâ hiçbir çip encode edilmedi**.
> ⚠️ **Ve bir ŞEMA BOŞLUĞU ÖLÇÜLDÜ:** §5.1 adım 9'un *"satırı encode edildi olarak
> işaretle"*sinin **karşılığı yok** — `tags`'te böyle bir sütun bulunmuyor (00004 +
> 00013; `status` **`unassigned`** kalmak **zorunda**, kartın kendi tuzağı). `Rows`
> portunun `MarkEncoded`'ı bu yüzden **tüketicinin ihtiyacını** beyan ediyor,
> B2c-2 nasıl karşılanacağına karar verecek.
>
> 🔴 **VE BU TURUN ÜRETTİĞİ YENİ AÇIK SİLİKON SORUSU — FAZ B3 İÇİN, BU LİSTEDE
> (denetim bulgusu N5: soru doğruydu ama yalnızca bir kod yorumunda ve denetim
> anlatısında duruyordu, yani FAZ B3'ü okuyan kişiye *"bunu ölç"* diyen satır yoktu):**
> **adım 5'in CommMode'u ÖLÇÜLMEDİ.** Tappa'nın `WriteData`'sı, dosya `02h` hâlâ
> **teslim haklarındayken** (`Write = ReadWrite = Eh`, veri sayfası **Tablo 8 s.12**)
> anahtar-0 oturumunda **CommMode.Full** gönderiyor; veri sayfası **§8.2.3.3 (s.12)**
> ve ikizi **§8.2.3.5 (s.13)** o durumda *"CommMode.Plain **has to be applied**"*
> diyor. **Hiçbir yayımlanmış örnek bu kombinasyonu kapsamıyor** — AN12196'nın Full
> `WriteData`'sı (§5.8.2) **teslim yapılandırmasında değil**: §5.4 (s.24) bunu birebir
> söylüyor ve Tablo 11 `00E0` → `Write = 0` (anahtar 0) çözüyor.
> ⚠️ **Çipin ne yaptığı da belgede yazmıyor:** iki bölüm de hangi **KİPİN** geçerli
> olduğunu söylüyor, çerçevenin **reddedildiğini değil** — Plain uygulayan bir çip
> mühürlü alanı **düz veri olarak dosyaya yazar**, yani tur yine düşer (yanıt
> MAC'inde) ama çip *"hâlâ boş"* **değildir**.
> 🔴 **VE B2c-2 İÇİN BİR BLOKLAYICI ŞART (2. güvenlik denetimi, R5, aynen):**
> *"`tenant_id` porta **AÇIK PARAMETRE** olmalı; bağlantının `SET LOCAL`'ine örtük
> bırakılırsa **kuşak (açık filtre) tamamen uygulamanın hafızasına kalır**."*
> `Rows.InsertUnassigned`'ın bugünkü imzası tenant taşımıyor ve `tags.tenant_id`
> **NOT NULL, DEFAULT'suz** (00004) — yani şekil B2c-2'de karara bağlanacak.
>
> **Ölçülecek:** gerçek bir çip adım 5'i kabul ediyor mu. **Yedek:** belgenin kendi
> **§5.8.1**'i (`ISOUpdateBinary`, CommMode.PLAIN). **Neden bloklamıyor:** adım 5 her
> `ChangeKey`'den **önce** koşuyor → hiçbir anahtar değişmemiş olur, plaket
> **kurtarılabilir** (§5.3 adım 5'ten yeniden koşup NDEF'i üzerine yazıyor — zaten
> ayırt etmediği çift). Aynı madde **ADR 0017 §6'ya `md. 12b` olarak** da yazıldı;
> kod tarafındaki tam kayıt `internal/encode/driver.go` → `roundSteps` başlığı.
>
> **Denetim turu (2026-08-21, bağımsız üçüncü göz — VERDICT: RED, beş bloklayan +
> altı bloklamayan; hepsi kapatıldı).** Denetçi ölçümlerin çoğunu kendi komutlarıyla
> yeniden üretti ve **brief'in adını verdiği yedi mutasyonun yedisini de** kırmızı
> buldu; **beş mutasyon hayatta kaldı** ve **dördü kanıt-iddiası sınıfıydı** —
> yanlış bayt değil, **hak ettiğinden fazlasını iddia eden cümle.**
>
> 🔴 **B1 — PLAKET ANAHTARI RASTGELE OLMAYI BIRAKABİLİYORDU VE BORU HATTI YEŞİL
> KALIYORDU.** `mintPlaqueKey`'in `rand.Read`'i sabit 16 baytla değiştirilince
> testler · `go vet` · `redline-check` **üçü de exit 0**. Yani her plaket **aynı**
> `K_SDMFileRead` ile çıkardı ve ADR 0005 risk 7'nin *görülebilir* dediği **tek bir
> dökümden** çıkan anahtar **tüm filo** için SUN üretirdi — ADR 0003 md. 3'ün tersi,
> sessizce. Asimetri kendi dosyamdaydı: **geçici** nonce için telden 16/16 ayrıklık
> ölçülüyordu, **kalıcı** anahtar için aynı döngü **hiç yazılmamıştı**.
> → `TestDriver_ThePlaqueKeyIsFreshOnEveryRound` (hem turdan hem doğrudan 64 çağrıdan)
> ve **`TestDriver_TheKeyInTheRowIsTheKeyOnTheChip`** — turun **ürettiği asıl şeyi**
> hiçbir şey doğrulamıyordu: satıra giden 44 baytın, **aynı KEK ve aynı UID AAD'si
> altında**, çipin benimsediği anahtara açılması. Üç yeni mutasyon (sabit anahtar ·
> yanlış AAD · satıra başka anahtar) artık kırmızı.
>
> 🔴 **B2 — TTL TAVANININ GEREKÇESİ UYDURULMUŞ BİR ATIFTI VE *"ÖLÇÜLDÜ"* ETİKETİ
> TAŞIYORDU.** *"Veri sayfası s. 28: alan düşünce kimlik doğrulama durumu ölür"*
> yazıyordu. **Yeniden ölçüldü** (97 sayfa, `pdftotext -layout`): `"authentication
> state"` **tam iki kez** geçiyor (§9.1.9 s.28, §9.1.10 s.29) ve **ikisi de komut
> hatası** hakkında; `"RF field"` · `"field is removed"` · `"power off"` ·
> `"deselect"` · `"HALT"` · `"leaves the field"` → **altısı da sıfır isabet**. Belge
> alanın düşmesi hakkında **hiçbir şey söylemiyor**. Yorum artık **DERIVED, NOT
> TRANSCRIBED** diye etiketli (`exchangeBudget`'ın kalibrasyonu), transkript olan
> yarı (*hatalı komut oturumu öldürür*) ile tasarım yargısı olan yarı **ayrıldı**, ve
> testin **hata mesajındaki** aynı cümle de düzeltildi.
>
> 🔴 **B3 — `Table 8` ADIYLA ANILIYORDU VE TABLO TERSİNİ YAZIYOR.** *"FileAR.Change
> ve FileAR.Write teslimde ikisi de anahtar 0"* deniyordu; Tablo 8 (**s. 12**, sayfa
> numarası da yanlıştı) dosya `02h` için `Read=Eh, **Write=Eh**, ReadWrite=Eh,
> Change=0h` veriyor — `Write` **serbest**, anahtar değil. Ve yanlış atıf **gerçek bir
> gerilimi** örtüyordu: **§8.2.3.3 (s.12)** *"If authenticated and the only access
> conditions satisfied are the free access Eh ones, then the **CommMode.Plain** is to
> be applied"* — oysa sürücü teslim ayarlarında **CommMode.Full** `WriteData`
> gönderiyor. **Gerilimi belge kendi yaparak çözüyor:** AN12196 rev. 2.0 **§5.8.2
> Tablo 17**'nin başlığı *"Write NDEF File - using Cmd.WriteData, **CommMode.FULL**"*,
> yeri **§5.6 (anahtar 0 ile auth) ile §5.9 (ChangeFileSettings) arasında** — yani
> dosya hâlâ teslim haklarında —, ve **başarılı R-APDU'yu basıyor**
> (`FC222E5F7A5424529100`, adım 21 yanıt MAC'ini **doğrulanmış** kaydediyor).
> ⚠️ **Açık kalan, dürüstçe:** yayımlanmış örnek silikon değildir; gerçek çip
> §8.2.3.3'ü harfiyen uygularsa adım 5 düşer ve yedek **§5.8.1'in `ISOUpdateBinary`
> CommMode.PLAIN**'idir — ucuz, çünkü adım 5 her `ChangeKey`'den önce koşar, yani
> reddeden çip hâlâ boştur. FAZ B3 ölçümü.
>
> 🔴 **B4 — *"ADR §4 aynı onu bağımsız sayıyor"* — İKİ FARKLI ON, TESADÜFEN EŞİT.**
> ADR §4'ün listesi `ChangeKey`'i **iki kez** sayıyor (adım 8 dahil) ve `GetCardUID`
> **taşımıyor**; bu tablo adım 8'i **sevk etmiyor** ve `GetCardUID` **ekliyor**. Biri
> düşmüş, biri eklenmiş → toplamlar **rastlantıyla** eşit, ve rastlantı çapraz
> doğrulama değildir. İki yorum ve testin hata mesajı düzeltildi; adım 8 sevk
> edildiği gün bu tablo **on bir** olacak, ADR'nin listesi **on** kalacak.
>
> 🔴 **B5 — *"AN12196 bunu tek oturumda yayımlıyor"* — İKİ AYRI OTURUM.** Ölçüldü:
> T17/T18'in `TI = 9D00C4DF`, T25/T26'nın `TI = 7614281A`. Ve **hiçbiri** bitişik bir
> `0,1,2,3` basmıyor: oturum B'nin basılı komutları `0000` (T21) → `0200` → `0300`,
> `0100` yalnız bir **yanıt** MAC girdisinde görünüyor. Yorum artık her iki izin **ne
> çivilediğini ayrı ayrı** yazıyor: oturum A *"komut başına bir artış"*ı (0000→0100,
> bitişik), oturum B *"sıfırdan farklı anahtarı değiştirmek oturumu düşürmüyor"*u
> (0200→0300). §9.1.2'nin muhasebe kuralı zaten doğru transkribe edilmişti.
>
> **Bloklamayanların hepsi kapatıldı:** **b6** — 🔴 **SEKİZİNCİ çıkış yolu vardı:
> panik.** `Step` `defer` kullanmıyordu; `advance` paniklerse `s.busy` **kalıcı true**
> kalır, süpürücü meşgulü **atlar**, `Abort` yalnız deadline'ı öne çeker → oturum, iki
> düz anahtar ve `perUID` yuvası **`Close`'a kadar** yaşardı (recover eden bir HTTP
> katmanının altında: süresiz) — §6 md. 7'nin **reddettiği** cevabın ta kendisi.
> `defer` eklendi, panik **yutulmuyor, yeniden fırlatılıyor**; `Store` yorumunun
> niteleyicisiz *"a property of the type"* cümlesi de **sekiz çıkışı sayarak** ve
> kapsamadıklarını (SIGKILL · core dump · swap) **adlandırarak** daraltıldı.
> ⚠️ Denetçi röle-kontrollü baytlarla gerçek panik **üretemedi**, yani bu bilinen bir
> açığı değil **ulaşılabilirliği bilinmeyen bir yolu** kapatıyor. · **b7** — oturum
> kimliği **sayaca** çevrilince suit yeşildi; `Step`/`Abort` **aktörü hiç kontrol
> etmiyor** (md. 10 açık), yani **ID tek yetkidir**, üstelik *"hamiline yazılı
> handle"* gerekçesi iki başka kararı da taşıyor. `TestSession_TheHandleIsUnpredictable`
> 64 örnekte **bayt konumu başına** en az 30 ayrık değer istiyor (rastgele için beklenen
> ~57; bir sayaç için baştaki baytlarda **1**). · **b8** — iki çıplak sayı çivilendi:
> `sweepDivisor` artık **istenen tick periyodundan** ölçülüyor (`6→1` kırmızı) ve
> `DefaultMaxLive` **bayt cinsinden anahtar malzemesi tavanıyla** (`64→100000` kırmızı).
> · **b9** — `keyVersion` yayımlanmış değere çivilendi. · **b10** — kart bu blokla
> **doğru sayılara** güncellendi. · **b11** — *"hiçbir ulaşılabilir durum"* cümlesi bir
> **izle** ölçülüyordu; artık durum uzayı **gerçekten** sürülüyor
> (`TestDriver_NoStateOfTheMachineEmitsChangeFileSettingsEarly`, tabloyu
> **çalıştırmadan** fonksiyon işaretçileriyle enumerate ediyor) ve eski alt-testin
> cümlesi sürdüğü ize daraltıldı.
>
> 🔴 **VE BU TURDA BİR ALTINCISINI KENDİM ARADIM (talimat gereği).** Kalan her
> *"ölçüldü/belge diyor ki"* cümlesi PDF'e karşı yeniden geçildi. Bir **yanlış alarm**
> çıktı ve kaydı önemli: `"freely accessible without secure messaging"` (§10.5.2)
> ilk taramada **sıfır isabet** verdi — çünkü cümle **satır sonunda bölünüyor**.
> Alıntı **doğru**; yanlış olan aramaydı. Düzeltilen tek gerçek hata **Tablo 8'in
> sayfa numarası** (11 → **12**) ve `GetVersion`'ın üç çerçeve cümlesinin **birebir**
> metni (*"The version data is return over three frames."*) oldu.
>
> **İkinci denetim turu (2026-08-21, YENİ bir üçüncü göz — VERDICT: RED, beş
> bloklayan + sekiz bloklamayan; hepsi kapatıldı).** Denetçi 1. turun düzeltmelerini
> doğruladı (B1 gerçek, B5 birebir, b9, M38'in meşruluğu, Tablo 8 s.12) ve kendi 22
> mutasyonunu attı; hepsi kırmızı.
>
> 🔴 **EN AĞIR BULGU BİR KOD KUSURU DEĞİL, BİR SÜREÇ HATASIYDI: *"düzelttim"* dediğim
> iki cümlenin İKİNCİ KOPYASI DÜZELTİLMEMİŞTİ.** B2'nin uydurulmuş atfı ve B4'ün geri
> çekilmiş çapraz kontrolü **testte** ayakta kalmıştı — ikincisi üstelik **canlı bir
> assertion** olarak. Bu, bu projenin **tekrar eden** kusuru (*"mekanizma taşındı,
> nesir ayakta kaldı"*). **Kural, bu turda uygulandı:** düzeltilen her cümle için
> `grep -rn` ile **önce ve sonra kopya sayısı ölçülür**. Bu turda düzeltilen yedi
> cümlenin yedisi de böyle ölçüldü; kalan isabetlerin **hepsi** geri-çekme
> anlatısının içindeki **alıntılar**.
>
> 🔴 **B-1 — BİR YARIŞ VE BİR ANAHTAR KAYBI YOLU.** `checkout`'un süre-doldu dalı
> **MEŞGUL** bir oturumu koşulsuz emekli ediyordu; üç kardeşinin (`sweepLocked`,
> `Abort`, aynı fonksiyonun ctx dalı) **üçü de** korurken.
> 🔴 **VE BU SAYIM DA EKSİKTİ: `Close` DÖRDÜNCÜ KARDEŞTİ** ve bu blok onu **hiç
> anmıyordu** — 3. denetimin tek bloklayanı (B-A) tam olarak oydu, ve şekli B4'ün
> *"iki farklı on"*uyla aynı: **kapalı bir sayım, eksik**. Aşağıya bakın.
> Ölçüldü: meşgul bir
> oturumun **beş anahtar tamponunun beşi de** — `K_SDMFileRead` dahil — ilk adım hâlâ
> `advance` içindeyken siliniyordu, ve `advance` **`st.mu` dışında** koşuyor — yani
> canlı anahtar malzemesi üzerinde bir **veri yarışı**; ayrıca `perUID` **tur
> ortasında** serbest kalıyordu. İkisi de gerçek. Kapatıldı;
> `TestStore_ABusySessionIsNeverWipedUnderItsOwner`.
> ⚠️ **DARALTILDI (3. denetim): bu bloğun ilk hâli sonucu fazla iddia ediyordu** —
> *"o pencereye düşen bir silme çipe sıfırlanmış bir anahtar kurar"*. Ölçüldü,
> **ulaşılabilir değil**: `zeroAll` `keyInventory` **sırasıyla** yürüyor
> (`K_SDMFileRead` **son**), `EV2ChangeKeyCommand` ise plaket anahtarını **önce**
> okuyor; iki somut deneme de fail-closed (tam sıfır → `sun.ChangeKeyData` reddediyor,
> yırtık anahtar → çip `911E`). Bu, **düzeltmenin içinde doğan** bir kanıt-iddiasıydı
> ve **üç kopya** hâlinde sevk edilmişti; üçü de daraltıldı. Kalıcı bozulmaya
> **gerçekten** ulaşan çağrı yeri **`Close`**'dur (B-A).
>
> 🔴 **B-5 — §6 md. 7 MADDE 3'ÜN GARANTİSİ, TURUN KORUMAK İÇİN VAR OLDUĞU TEK ANAHTAR
> İÇİN TUTMUYORDU.** Yorum *"her çıkış yolu siler … sızdırabilecek bir dal yok"*
> diyordu; ölçüldü: `ring.add`'i `WrapKey`'den **sonraya** almak ve başarılı
> `InsertUnassigned`'dan **sonraya** almak **ikisi de yeşil** kaldı. İkincisiyle,
> **başarısız bir satır yazımı** — paketin zaten testi olan bir yol — taze basılmış
> AES-128 plaket anahtarını heap'te **kayıtsız** bırakıyordu. Boşluk **yapısaldı**:
> her silme iddiası yalnız adım 3'ün **başarılı** olduğu yollarda dilim başlığı
> tutuyordu, iki hata testi ise **anahtarın baytlarına hiç bakmıyordu**. Kapatıldı:
> `capturingWrapper` port argümanından dilim başlığını yakalıyor ve
> `TestDriver_TheFreshPlaqueKeyIsWipedOnBothReachableFailurePathsOfStep3` **iki hata yolunda**
> baytların sıfır olduğunu, **pozitif kontrolde** ise anahtarın adım 6'ya kadar
> **canlı kaldığını** ölçüyor.
>
> 🔴 **B-4 — 1. TURUN B3 "ÇÖZÜMÜ" ÖLÇÜLEBİLİR ŞEKİLDE YANLIŞTI VE GERİ ÇEKİLDİ.**
> *"AN12196 §5.8.2 Tablo 17 teslim haklarında Full `WriteData` yapıyor"* demiştim.
> Kendi ölçümüm de denetçininkini doğruladı: **§5.4 (s.24) birebir** *"This step does
> **not** reflect default delivered NTAG 424 DNA configuration of NDEF file settings
> (0000E0EE00010026000CA)"*, ve **Tablo 11 (s.24)** örnek çipin `AccessRights = 00E0`
> olduğunu, yani `FileAR.Write = 0` (**anahtar 0**) olduğunu çözüyor — §8.2.3.3'ün
> *"yalnız serbest `Eh` koşulları"* şartı orada **hiç tetiklenmiyor**. Konumsal
> argüman da düşüyor: **§5.4, §5.8'den ÖNCE** geliyor. **Sonuç: gerilim AÇIK.**
> Tappa'nın adım 5'i teslim haklarındaki (`Write = ReadWrite = Eh`) bir dosyaya
> anahtar-0 oturumunda **CommMode.Full** yazıyor ve **hiçbir yayımlanmış örnek bu
> kombinasyonu kapsamıyor**. Ayakta duran yarı korundu, fazla iddia eden **silindi**:
> adım 5 her `ChangeKey`'den **önce** koşar, yani reddeden çip **hâlâ boştur** ve
> yedek belgenin kendi **§5.8.1**'i (`ISOUpdateBinary`, CommMode.PLAIN). Hangisini
> silikonun kabul ettiği **FAZ B3 ölçümüdür**. ⚠️ *"Bir gerilimi açık bırakmak,
> yanlış kapatmaktan iyidir."*
>
> **Sekiz bloklamayanın hepsi:** **n1** — *"Step ve Close'daki sekiz çıkış"* kapalı
> sayımı **yanlıştı** (B4'ün iki onuyla aynı şekil): `grep` **on bir** `retireLocked`
> çağrı yeri sayıyor, ikisi o sekizin dışında (`finishLocked`'ın *"adım koşarken
> doldu"*u ve `Begin`'in ilk komut hatası). Sayı **kaldırıldı**, küme adlandırıldı; ve
> adı *"Every"* diyen test **gövdesi sekiz süren** adıyla yeniden adlandırıldı
> (`TestStore_TheDrivenExitPathsWipeEveryKeyBuffer`, üç kopyanın üçü de güncellendi).
> · **n2** — `sweepDivisor`'ın takas cümlesi **tersine** yazılmıştı (küçük bölen =
> **uzun** periyot = **geniş** pencere), üstelik bir önceki denetimin düzelttirdiği
> blokta; cümle düzeltildi **ve** daraltma yönü de bağlandı (`6→12` artık kırmızı).
> · **n3** — `DefaultTTL` *"ilerleme olmadan"* diyordu ama deadline `Begin`'de bir kez
> kuruluyor ve **asla ilerletilmiyor**; **toplam tur bütçesi** olarak yeniden yazıldı,
> operasyonel bedeli (90 sn'yi aşan meşru tur **adım 6'dan sonra** ölebilir, ama
> fail-closed ve kurtarılabilir) yazıldı, ve
> `TestSession_TheDeadlineIsATotalBudgetAndIsNeverExtended` iki okumadan **birini**
> çiviledi. · **n4** — sahte çip `WriteData`'nın **Offset ve Length**'ine kördü (bir
> önceki turda **kendi bulduğum** dosya-numarası zaafının aynı sınıfı, bir alan
> ötede); çip artık üç başlık alanını da çözüyor, uzunluğu gövdeye karşı doğruluyor,
> ofsete yazıyor, ve `chip_test.go`'nun *"başlık bölünmesi doğru"* iddiası **hangi
> komut için ne kadar** doğrulandığını söyleyecek şekilde daraltıldı. · **n5** —
> `perUID`'nin kimlik guard'ı çivisizdi ve **B-1 çifte retire'ı ulaşılabilir kılıyordu**
> (ikisi birleşiyor); `TestStore_RetiringOneSessionDoesNotFreeAnotherPlaqueSlot`.
> · **n6** — *"tek ve meşru"* kesin tekili **kaldırıldı**; hayatta kalanlar artık
> **örneklem** olarak sunuluyor ve **üçü ayrı ayrı** gerekçeli. · **n7** — Tablo
> 17'nin *"is titled"*'ı: başlık `Write NDEF File - using Cmd.WriteData`,
> `, CommMode.FULL` **§5.8.2 bölüm başlığına** ait — uydurma değil **imprecision**,
> ama B3'ün üstünde yükseldiği fiilin aynısı; kaydedildi. · **n8** — `staticcheck`
> **PATH'te yok** ve öyle olması gerekmiyor: `Makefile`'ın `lint` hedefi (satır 346)
> gibi **`go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...`** ile koşuluyor;
> komut artık raporda yazılı.
>
> **Üçüncü denetim turu (2026-08-21, ÜÇÜNCÜ ayrı üçüncü göz — VERDICT: RED, BİR
> bloklayan + yedi bloklamayan; hepsi kapatıldı).** Bloklayan sayısı **5 → 5 → 1**.
> Denetçi süreç hatasının **temiz kapandığını** doğruladı (`counts the same ten` 0 ·
> `RESOLVED BY THE DOCUMENT DOING IT` 0 · `without progress` 0; kalan dört isabet
> kendi geri-çekilme bloklarının içindeki **alıntılar**), B-4'ün üç ölçümünü PDF'ten
> **birebir** yeniden üretti, B-5 ve n4'ün gerçekten kapandığını mutasyonla gösterdi,
> ve `capturingWrapper` itirazını **kendi ölçümüyle geri çekti** (dilim **başlığını**
> tutuyor, kopya değil → sözleşmeye uyan bir sahteden **daha güçlü**). Kalıcı değer
> süpürmesinde **üçüncü bir boşluk bulunamadı**.
>
> 🔴 **B-A — `Close` B-1'in DÖRDÜNCÜ KARDEŞİYDİ VE KORUMASIZ BIRAKILMIŞTI; SONUCU
> SATIRA 16 SIFIR BAYT.** `Close` her canlı oturumu `s.busy` bakmadan emekli
> ediyordu. Kendi sondamla yeniden ürettim: meşgul bir oturumun **beş tamponunun
> beşi de** silindi. Denetçinin uçtan uca ölçümü daha ağır: gerçek zaman harcayan bir
> `Wrapper` ile silme **`sun.Wrap`'ın içine** düşüyor ve satır, açıldığında **on altı
> sıfır bayt** veren bir `aes_key_ref` ile **COMMIT** ediliyor. Hiçbir şey reddetmiyor
> — `sun.Wrap`'ta sıfır kapısı yok, `InsertUnassigned`'da yok, turun tek sıfır kapısı
> `sun.ChangeKeyData`'da ve **üç alışveriş SONRA**. ADR 0003 md. 3 sessizce ve
> **kalıcı** olarak yeniliyor (`tappa_app` o sütunu asla yeniden yazamaz, 00013).
> 🔴 **VE ÇÖZÜM KARDEŞLERİN KOPYASI DEĞİL, BİR KARAR:** kardeşler *"atla, sahibi
> bitirsin"* diyebiliyor çünkü **sahip var**; kapanışta sahip **gidiyor**, yani
> *"atla"* anahtarı sürece bırakmak olurdu — §6 md. 7'nin **reddettiği** cevap.
> Seçilen üç parça: **(1)** boştaki oturumlar **hemen** siliniyor · **(2)** uçuştakiler
> için `CloseGrace` (varsayılan **5 sn**, yani `exchangeBudget`
> mertebesinde — bir turun değil bir **adımın** dönmesini bekliyor) kadar
> **BEKLENİYOR**, sonra siliniyor · **(3)** süre dolarsa **SİLİNMİYOR** ve `Close`
> **hata döndürüyor**. Üçüncüsü ters görünüyor ve değil: sahibin altından silmek
> **kalıcı ve sessiz** bir bozulma (sıfır `aes_key_ref`), silmemek ise **geçici ve
> GÜRÜLTÜLÜ** bir maruziyet — sayı çağırana dönüyor. ⚠️ O dalda §6 md. 7 madde 3'ün
> garantisi o oturumlar için **tutmuyor** ve `Close` bunu `nil` döndürmek yerine
> **hatasında söylüyor**. Ayrıca *"Close dönünce bu süreçte hiçbir şey bir oturum
> tutmuyor"* cümlesi **ölçülerek yanlışlandı** ve artık yalnız `Close` `nil`
> döndürdüğünde doğru. `Close()` imzası `error` döndürüyor; her çağrı yeri uyarlandı,
> yani drenaj edilemeyen bir oturum artık **test hatası**.
>
> **Yedi bloklamayanın hepsi:** **N1** — 🔴 **B-1'in KENDİ sonuç cümlesi fazla iddia
> ediyordu ve ÜÇ kopya hâlinde sevk edilmişti** (*"o pencereye düşen bir silme çipe
> sıfırlanmış bir anahtar kurar"*). Ölçüldü, **ulaşılabilir değil**: `zeroAll`
> `keyInventory` **sırasıyla** yürüyor (`K_SDMFileRead` **son**), `EV2ChangeKeyCommand`
> plaket anahtarını **önce** okuyor; iki somut deneme de fail-closed (tam sıfır →
> `sun.ChangeKeyData` reddediyor; yırtık anahtar → çip **`911E`**). Yarış ve `perUID`'nin
> tur ortasında bırakılması **gerçek**; adı konan sonuç değil. **Üç kopya birden**
> daraltıldı, `grep -rn` ile önce/sonra ölçüldü. ⚠️ Bu, **düzeltmenin içinde doğan**
> yeni bir kanıt-iddiasıydı — sınıf üçüncü kez tekrarladı. · **N2** — iki gerçek boşluk:
> **`A3b`** §5.1 **adım 9'un sırası** (*"Zero(keys) FIRST … then mark"*) **çivisizdi**,
> oysa §5.2'nin ikiz sıra iddiası ilk turdan beri çivili — asimetri B1'inkiyle aynı;
> artık `MarkEncoded`'ın **içinden** ölçülüyor (`TestDriver_Step9WipesTheKeysBeforeItMarksTheRow`).
> 🔴 **Ve o testin ilk hâli mutasyonu kaçırdı**: her çağrıda **üzerine yazıyordu**, yani
> satırı iki kez işaretleyen bir mutant suçlayıcı gözlemi masumla eziyordu; artık
> **tüm** gözlemler saklanıyor. **`A4`** — `keyring.take` bir **KOPYA** döndürebiliyordu
> ve suit yeşildi; kopya halkadan tamamen kaçar, `zeroAll` ona **asla** ulaşamaz. Altı
> test `peek`'e demirlenmişti, `take`'e **hiçbir şey** — üçüncü kez aynı şekil. Artık
> ikisi de **takma ad** özelliğiyle çivili. **`A18`** — `s.finished` de çivilendi.
> · **N3** — *"EVERY"* diyen bir test adı **iki** yol sürüyordu; üçüncüsü
> (`ring.add`'in hatası) **ulaşılabilir değil** (taze oturumda slot boş) — ad
> `…OnBothReachableFailurePathsOfStep3` oldu ve gerekçe yazıldı. Aynı turda düzelttiğim
> ad-gövde kusurunun **bir test ötesinde** tekrarıydı. · **N4** — `driver.go` belgenin
> söylemediği bir **silikon davranışı** iddia ediyordu (*"refuses it"*, *"still blank"*);
> §8.2.3.3 ve §8.2.3.5 hangi **KİPİN** geçerli olduğunu söylüyor, çerçevenin
> **reddedildiğini değil** — Plain uygulayan bir çip mühürlü alanı **dosyaya yazar**.
> İki fiil düzeltildi; **güvenlik sonucu (kurtarılabilirlik) ayakta**, çünkü adım 5 her
> `ChangeKey`'den önce koşuyor. · **N5** — turun ürettiği **tek açık silikon sorusu**
> yalnız bir kod yorumunda ve denetim anlatısındaydı; artık **kartın kendi "AÇIK
> KALAN" listesinde** ve **ADR 0017 §6'da `md. 12b`** olarak. · **N6** — sahtenin
> *"ne kanıtlamaz"* listesi, sahtenin **açık sorunun izin veren dalını modellediği**
> tek yeri atlıyordu (hiçbir erişim hakkı / CommMode kuralı uygulamıyor); madde
> **adıyla** eklendi. · **N7** — veri sayfası **97 sayfa** (`pdfinfo`), *"98"* değil;
> koddaki ve karttaki iki kopya da düzeltildi.
>
> ⚠️ **VE BU TURDA `redline-check` BİR KEZ KIRMIZIYA DÖNDÜ — SEBEBİ BENDİM.** `Close`'un
> yeni hata metni `tags.aes_key_ref`'i **adıyla** anıyordu ve R7'nin `aes_?key`
> tetikleyicisine takıldı. **Yanlış pozitifti** (argümanlar bir `int` ve bir
> `Duration`), ama doğru cevap **muafiyet değil**: hata mesajı **kısaltıldı**, gerekçe
> zaten `Close`'un doküman yorumunda. `redline-check.sh` yeniden **exit 0**.
>
> **Güvenlik denetimi turu (2026-08-21, `tappa-security-auditor` — VERDICT: RED, bir
> YÜKSEK + dört DÜŞÜK; hepsi kapatıldı).** Kanıtıyla **temiz** bulunanlar: **R1**
> biyometri 0 · **R2** GPS 0 · **R3** `transactions` dokunulmamış · **R4** (`useCtr`
> yalnız `advance` içinden ve `busy` dışlaması altında; `perUID` yalnız kimlik
> eşleşince; `0xFFFF` reddediliyor) · **R6/§5.2** sıra doğru (satır, çipin ilk geri
> döndürülemez komutundan **dört değiş-tokuş önce**; hata yolunda satırı temizleyen
> **hiçbir dal yok**) · **R7 yarış** (60 iterasyonluk stres sondası `-race` temiz; **11
> `retireLocked` çağrı yerinin her biri** ya sahibinin kendisi ya `s.busy` korumalı) ·
> **R7 sızıntı** (üretimdeki **her** format argümanı ölçüldü; 32+ haneli hex sabit
> yok) · silme bir **mekanizma** (`zeroAll` silinince **10+ test** kırmızı) · risk 7'ye
> karşı **fazla iddia yok** · **M47 gerekçesi denetçinin kendi mutasyonuyla
> doğrulandı**.
>
> 🔴 **YÜKSEK — VE BULUNAN ŞEY BENİM BİR ÖNCEKİ TURDAKİ *DÜZELTMEMİN* İÇİNDEYDİ:
> `Close`'un grace'i dolduğunda kalan oturum HİÇ silinmiyordu.** Ne o an, ne sahibi
> dönünce, ne sonra. Kendi sondamla birebir yeniden ürettim (bloklayan bir `Wrapper`
> ile tur `getversion.3` içinde tutuldu, `CloseGrace=50ms`): `Close` hata döndürdü,
> uçuştaki `Step` **başarıyla** tamamlandı, ve **düz AES-128 plaket anahtarı hâlâ
> bellekteydi** — `busy=false`, `finished=false`, süpürücü **ölü**. Mekanizma:
> `retireIdleLocked` meşgul oturumu sayıp **`continue`** ediyordu ve **deadline'a
> dokunmuyordu**, oysa meşgul bir oturum için kalan tek retire sebebi
> `finishLocked`'ın deadline kontrolü. Paket içinde o oturuma ulaşan **hiçbir yol**
> kalmıyordu.
> 🔴 **VE İKİLEM SAHTEYDİ — üçüncü seçenek aynı dosyada zaten iki kez kullanılıyor.**
> `Abort` ve `checkout`'un ctx dalı meşgul oturumun **deadline'ını ŞİMDİ'ye çekip**
> silmeyi **sahibinin kendi `finishLocked`'ına** bırakıyor. `Close` artık aynısını
> yapıyor: **tek satır** (`s.deadline = now`), sahibin altından silme **yok**, `-race`
> temiz. `Close`'un **hata sözleşmesi de değişti**: artık *"silinmedi"* değil
> **"HENÜZ silinmedi — sahibi döndüğünde silinir"*, ve kalan gerçek **daha dar ve
> adıyla yazılı**: **hiç dönmeyen** bir sahip (sonsuza kadar bloke bir port çağrısı)
> halkasını canlı tutar — tıpkı kalıcı sıkışmış herhangi bir goroutine gibi.
> `CloseGrace` bunu **sınırlamıyor** ve bu yazılı.
>
> 🔴 **ÜÇ FAZLA-İDDİA CÜMLESİ YİNE BENİM O TURDAKİ YENİ CÜMLELERİMDİ** — sınıf
> **üçüncü kez** düzeltmenin içinde doğdu (tur 2'de B-4, tur 3'te N1, şimdi bu):
> *"transient loud exposure"* (**geçici değildi**; eşik bir **uçurumdu**: grace−1ms'de
> silinir, grace+1ms'de asla) · `Close`'un artık kabulünün **sahibin dönüşünden
> sonrasını** söylememesi · ve *"**exactly the eleven** call sites of `retireLocked`"*.
> 🔴 **BU SONUNCUSU ÜÇÜNCÜ KAPALI-SAYIM KUSURUYDU** (B4'ün *"iki farklı on"*u ·
> `Close`'un *"üç kardeş"*i · şimdi *"tam on bir"*) ve sayı artık **nesir değil**:
> `retireCallSites` bir **envanter**, `TestSession_TheRetireCallSitesAreInventoried`
> onu **go/ast ile sayıyor**, iki yönlü ratchet (12. çağrı yeri → **kırmızı**).
> **Kural: kapalı bir sayım yazacaksan saymayı da mekanikleştir, yoksa yazma.**
>
> **Dört DÜŞÜĞÜN hepsi:** **L1** — panik dalı `s.busy`'yi **temizlemiyor** ve drain
> sinyali **vermiyordu**; tek meşgul oturum paniklerse `Close` boşuna tam grace
> bekleyip *"**0** encode session(s) … NOT wiped"* diye **yanlış bir §4.7 artık
> alarmı** veriyordu. İkisi de artık **`retireLocked`'ın içinde**, yani her retire
> yolu aynı şekilde; birim **ve** davranışsal testle çivili. · **L2** —
> `ring.add(keyNameSesENC, …)` hata verirse **`auth.KeyMAC` HİÇ kaydedilmiyordu** →
> `zeroAll` ona asla ulaşamaz (`add` yalnız **kendi reddettiğini** siler). Bugün
> ulaşılamaz (ölçüldü) ama **adım 8 aynı kalıbı İKİ anahtarla tekrarlayacak**; iki
> `add` de artık **koşulsuz deneniyor** ve doğrudan çağrıyla çivilendi. · **L3** —
> `InsertUnassigned` imzasında **`tenant_id` yok**; `tags.tenant_id` **NOT NULL,
> DEFAULT'suz** (00004). Denetçinin asıl noktası bir **tutarsızlıktı** —
> `MarkEncoded` eksik sütunu **ölçüp adlandırıyor**, aynı titizlik tenant'a
> uygulanmamıştı. 🔴 **VE BU MADDE O TUR KAPATILMADI — kart bunu YANLIŞ yazdı;
> düzeltmesi ve nasıl kaçtığı bir sonraki blokta (B4).** · **L4** —
> `DefaultMaxPerActor`'ın *"hostile with a valid handle"* cümlesi yanlış okumaya davet
> ediyordu: `actor` **çağıranın verdiği bir dizedir** (ölçüldü: yalnız sayacın
> anahtarı), yani düşman onu değiştirip taze bütçe alır; **gerçek tavan
> `DefaultMaxLive`**'dır. Cümle daraltıldı. ✅ *"`actor` yetkiyi değil MARUZİYETİ
> sınırlar"* iddiası **doğrulandı**.
>
> ⚠️ **Denetçinin kendi niteleyicisi, aynen:** *"Uzaktan sömürülebilir DEĞİL — bellek
> okuma (core dump, swap, debugger) gerektirir; ve BUGÜN üretimde erişilemez:
> `internal/encode` hiçbir yerden import edilmiyor (paket dışı import sayısı **0**).
> Bloklama sebebim erişilebilirlik değil, **turun KENDİ kabul kriterinin kırılması**."*
>
> **Dördüncü denetim turu (2026-08-21, DÖRDÜNCÜ ayrı üçüncü göz — VERDICT: RED, beş
> bloklayan + dört bloklamayan; hepsi kapatıldı).** Denetçinin teşhisi: *"**kod
> yakınsıyor, iddialar yakınsamıyor**"* — en ağır beşin **dördü kanıt hakkındaki
> cümleler**, biri kanıtsız bir guard, ve **üçü yine bir önceki turun düzeltmesinin
> içinde** doğdu (**dördüncü ardışık tur**). Doğrulanıp **tutan**: build/vet/gofmt/
> staticcheck · ölçümlerin tamamı · **veri sayfası 97 sayfa** ve ~20 alıntının hepsi
> PDF'ten birebir · `retireCallSites` ratchet'i **gerçekten iki yönlü** (11→12 ve
> silme, ikisi de kırmızı) · denetçinin **32 bağımsız mutasyonundan 27'si kırmızı**.
>
> 🔴 **B4 — VE BU EN AĞIRI, ÇÜNKÜ ÜÇÜNCÜ KEZ AYNI SÜREÇ HATASI: L3 KAPATILMAMIŞTI VE
> "KAPATTIM" DİYE RAPORLANMIŞTI.** `grep -c tenant_id internal/encode/session.go` →
> **1**, ve o tek isabet md. 10 hakkındaydı; `InsertUnassigned`'ın yorumunda
> `tenant_id` **hiç geçmiyordu**. Sebep **mekanikti ve kaydedilmeli**: L2/L3/L4'ü tek
> bir toplu script'te uygulamıştım, script **sona kadar yazmıyor** ve L4'ün anchor'ı
> tutmayınca `AssertionError` ile düştü → **L2 ve L3 de yazılmadı**; L4'ü ayrıca
> uyguladım, L3'ü unuttum ve **doğrulamadan** kapandı dedim. ⚠️ Kart da yanlış yazdı.
> 🔴 **Ve var olan tek tenant cümlesi şemaya aykırıydı:** `Begin`'in *"the row lands in
> whatever tenant `app.tenant_id` names"*'i — `tags.tenant_id` **NOT NULL,
> DEFAULT'suz** (00004), yani argümansız bir INSERT hiçbir yere *"düşmez"*, **patlar**.
> İkisi de yazıldı ve **komutla doğrulandı** (`InsertUnassigned` doküman bloğunda
> tenant_id: **0 → 5** isabet). **Kural artık: bir maddeyi kapattığını yazmadan önce
> kanıtını KOMUTLA üret ve komutu raporda göster.**
>
> 🔴 **B1 — `Close`'un YENİ kanıt cümlesi ikinci bir `Close` için yanlıştı, ve artık
> tek artık değildi.** `if st.closed { return nil }` yüzünden **ikinci** `Close`
> **nil** dönüyordu; o anda oturum **canlı**, **busy** ve halkası **sıfırlanmamış düz
> plaket anahtarını** tutuyor. Ve bu ezoterik değil: `newHarness`'ın `t.Cleanup`'ı her
> testte `Close` çağırıyor, yani **açıkça kapatan her test zaten çift-kapatıyordu**.
> Artık yalnız **tek seferlik teardown** korunuyor; retire-and-drain yarısı **her
> çağrıda** koşuyor → `Close` hem **idempotent** hem **dürüst**. Artık **tek** kalem:
> *hiç dönmeyen* bir sahip.
>
> 🔴 **B2 — `TestStore_CloseDoesNotCommitAnAllZeroKey` BOŞTU; hiçbir şey iddia
> etmiyordu.** Gövdesi `h.rows.inserted` üzerinde dönüyordu ve o küme **30/30 koşuda
> boştu** — `Close` goroutine doğar doğmaz çağrıldığı için tur **adım 3'e hiç
> ulaşmıyordu**; B-A kusuru geri konduğunda test **60/60 yeşil** kalıyordu. Oysa
> güvenlik denetiminin bulduğu şeyin **kalıcı** yarısını (satıra sıfır `aes_key_ref`)
> çivileyen tek test oydu. Yeniden yazıldı: `blockingWrapper` turu **`sun.Wrap`'ın
> içinde** park ediyor — kusurun ihtiyaç duyduğu tam pencere — ve **boş küme kontrolü**
> eklendi. Ölçüm: B-A mutasyonu (M59) artık **bu test tarafından** kırmızıya
> çevriliyor.
>
> 🔴 **B3 — L1'in "davranışsal" yarısı kendi senaryosunu HİÇ sürmüyordu.** L1 düzeltmesi
> silindiğinde alt-test **20/20 yeşil**di; yalnız birim assertion'ı kırmızıydı. Yorumu
> *"sequenced so Close is already waiting"* diyordu ama sıralama **hiç oluşmuyordu**.
> Denetçinin ölçtüğü 200 ms'lik sıralama eklendi → mutasyon **5/5 kırmızı**, ve
> uykunun **neden** gerekli olduğu (üretime test kancası eklemeden gözlenebilir bir
> kenar yok) ile **neden yanlış yönde flake edemeyeceği** yazıldı.
>
> 🔴 **B5 — `advance`'in `s.finished` guard'ı çivisizdi ve testi YANLIŞ SEBEPLE
> yeşildi** (M2-08'in genellemesi, **üçüncü** vaka). `s.finished ||` silindiğinde tüm
> suite yeşil kalıyordu, çünkü test `sw(0x9100)` veriyordu ve `stepIdx=0`'da
> `RequireStatus` **önce** düşüyordu — assertion **durum sözcüğü kapısıyla**
> karşılanıyordu. Girdi, makinenin **kabul edeceği** `sw(0x9000)` yapıldı (+ canlı
> oturumda **pozitif kontrol**); M60 artık kırmızı. Geçen tur bayrağın **yazılması**
> çivilenmişti, **okunması** değil.
>
> **Dört bloklamayan:** **N1** — `DefaultCloseGrace` turun tek çivisiz boyut sabitiydi
> (`5s→90s` ve `5s→1ns` ikisi de yeşil); yorumunun **iki ilişkisel iddiası** artık
> assertion (`>= exchangeBudget`, `< DefaultTTL`, ve `<= DefaultTTL/3`), M61/M62
> kırmızı. · **N2** — sahtenin `applyChangeKey`'i Tablo 63'ün **iki gövde biçimini**
> yalnız `len(plain)`'e bakarak seçiyordu, **`keyNo == authKeyNo`'ya asla** — yani
> gerçek çipin **reddedeceği** bir biçimi kabul ediyordu; bu, aynı sahte zaafının
> **üçüncüsü** (dosya numarası → `WriteData` ofseti/uzunluğu → gövde biçimi) ve
> **adım 8 tam oraya yürüyecek**. Karşılaştırma eklendi, başlıktaki niteleyici
> düzeltildi. · **N3** — *"`retireCallSites` **below**"* yanlış yönlendiriyordu
> (`session_test.go`'da); düzeltildi. · **N4 (denetçi DOĞRULANAMADI diye etiketledi)**
> — `Close`'un `select`'inde timer ile drain aynı anda hazırsa `len(st.live)` **retire
> sonrası** okunup *"0 encode session(s)"* basabilirdi. **Ölçtüm:** 1 ms grace'e 1 ms'de
> bırakılan drain sinyaliyle **400 denemede 0** yanlış alarm — yani pencereyi **ben de
> üretemedim**. Yine de sayı artık `len(st.live)` yerine **taze bir sweep'ten**
> türetiliyor, yani pencere **yapısal olarak** kapatıldı; M63'ün hayatta kalması bu
> yüzden **beklenen ve meşru** (aşağıda sayılı).
>
> 🔴 **BU TURUN DERSİ, VE ARTIK BİR KURAL:** bu görevde **dört kez** bir test kendi
> yorumunun anlattığı senaryoyu **sürmedi** (`…EveryExitPath…` · `…OnEVERYFailurePath…`
> · B2 · B3). Bir test yazarken sorulacak soru *"geçiyor mu"* değil, **"koruduğu şey
> bozulsa kırmızıya döner mi"** — ve cevap **okumayla değil mutasyonla** verilir. Bu
> turda yazılan/değiştirilen her testin mutasyonu koşuldu.
>
> **Beşinci denetim turu (2026-08-21, BEŞİNCİ ayrı üçüncü göz — VERDICT: RED, dört
> bloklayan + üç bloklamayan; hepsi kapatıldı).** 🔴 **Yön veren cümle denetçinin
> kendisinden:** *"üretim yolunda (`session.go`/`driver.go`) **yeni bir
> güvenlik/doğruluk kusuru BULAMADIM** — 21 mutasyonun 20'si kırmızı."* **Kod
> yakınsadı;** kalan dördün biri **sahte çipte**, ikisi **kanıt cümlesi**, biri
> **kartta**.
>
> 🔴 **B1 — BU TURDA EKLEDİĞİM `ChangeKey` KAPISI YANLIŞ KURALI KODLUYORDU, ve adını
> verdiği tablo TERSİNİ yazıyor.** Kendi ölçümüm (md5 `dcf5319c…`, 97 s.) denetçiyi
> doğruladı: **Tablo 63 (s.62)** ayrımı **ANAHTAR NUMARASIYLA** yapıyor — *"if key 0
> is to be changed"* (17 bayt) / *"if key 1 to 4 are to be changed"* (21 bayt).
> `"KeyNo == AuthKey"` çerçevesi **AN12196'nın BÖLÜM BAŞLIĞI** dili. Depo zaten
> **doğrusunu** yazıyordu ve danışılmamıştı (`internal/sun/changekey.go`:
> *"THE SWITCH IS ON THE KEY NUMBER"*; ADR 0017 §5.1: *"kod **numaraya** bakmalıdır,
> oturuma değil"*). Ve bu yalnız atıf değildi: yanlış kural **ADR'ye UYAN kodu
> reddediyordu** — `0x01` ile kimlik doğrulayan bir oturum (**§5.3 probe 2'nin tam
> şekli**, B2c-2'nin koşacağı) Tablo 63'ün doğru 21 baytlık gövdesini üretiyor,
> sahte çip testi düşürüyordu. **Yedi kopya + kart** düzeltildi (`grep -rn` önce
> **12** isabet / sonra yalnız **geri-çekme alıntıları**). 🔴 **Ve denetçi ölçmüştü:
> eski satır geri alınınca suite YEŞİL** — yani hiçbir şey satın almıyordu. Artık
> **Tablo 65'in ön koşulu da uygulanıyor** (`AUTHENTICATION_ERROR AEh`, *"missing
> active authentication with AppMasterKey"*; §8.2.4.1) ve **beş vakalık ayırt edici
> bir test** kapıyı çiviliyor (M65 kırmızı). ⚠️ Tablo 65 uygulanınca iki ölçüt
> **çakışıyor**, yani M64 **meşru olarak hayatta kalıyor** — ADR 0017 §5.1'in zaten
> söylediği şey, ve kod yine de **numaraya** bakıyor.
>
> 🔴 **B2 — `DefaultTTL`'in *"asla uzatılmaz; ona yazan her şey ONU ÖNE çeker"*
> cümlesi ölçülerek yanlışlandı: KAPALI SAYIM, bir eksik.** Denetçi üçüncü yazarı
> buldu (`retireIdleLocked`) ve **korumasız** olduğunu ölçtü: bütçesi bir dakika önce
> dolmuş bir oturumda `Close` deadline'ı **tam bir TTL ileri** itiyordu. 🔴 **Ve o
> yazar için yazdığım test hemen bir DÖRDÜNCÜSÜNÜ buldu** — `checkout`'un iptal dalı
> aynı şekildeydi. Site yamamak site saymaya yeniliyordu, o yüzden çözüm **yapısal**:
> tek bir `expireLocked` yardımcısı, üç çağrı yeri, ham atama **sıfır**. Cümle artık
> bir **özellik**, ve `TestSession_EveryWriterOfTheDeadlineMovesItEarlier` **üçünü de**
> sürüyor (M66 üç alt-testin üçünde birden kırmızı).
>
> 🔴 **B3 — 200 ms sıralamasının YENİ gerekçesi (*"tek yönde güvenli"*) kategorik
> olarak yanlıştı, ve denetçi bunu ÜRETİM KODUNU MUTASYONA UĞRATMADAN gösterdi:**
> uykuyu `CloseGrace`'in üstüne çıkarmak testi düşürüyor. Yarısı doğruydu (kısa uyku
> yalnız daha azını kanıtlar); yanlış olan **imkânsızlık** iddiasıydı. Cümle bir
> **sayılmış marja** çevrildi ve marj büyütüldü: `CloseGrace` 5 s → **30 s**, uyku
> 200 ms → **150×**.
>
> 🔴 **B4 — kartın hayatta kalanlar bloğu BEŞİNCİ kapalı-sayım kusuruydu:** başlık
> *"ÜÇÜ DE"* diyordu, gövdede **dört** madde vardı ve `3'.` numarası M63'ün başlığa
> dokunmadan araya sokulduğunu ele veriyordu. Başlık **sayıdan arındırıldı**
> (*"HER BİRİ"*), numaralar düzeltildi, ve beşinci madde eklendi.
>
> **Üç bloklamayan:** **n1** — N4 yorumunun adlandırdığı mekanizma
> **gerçekleşemez** (`retireLocked` `delete(st.live, …)` yapıyor, emekli oturum
> sayılamaz); **sertleştirme doğru, hikâye yanlıştı** — düzeltildi. · **n2** — kapsam
> sayısı **deterministik değil**: beş ardışık koşu **93,1 / 92,6 / 92,6 / 92,6 /
> 92,6** (kendi ölçümüm), yani raporlanan sayı **mod**; karta böyle yazıldı.
> · **n3** — `driver.go`'nun aynı yanlış adlandırması B1 ile birlikte düzeltildi.
>
> ⚠️ **DENETÇİNİN AÇIKÇA ÖLÇMEDİĞİ İKİ ŞEY, benim tarafımdan koşuldu:** DB sondası
> (`BEGIN READ ONLY … ROLLBACK`, oturum kapatıldı) → `04AC7E55000601 = active`,
> **139204 / 448234**, değişmedi · ve mutasyon toplamı **baştan yeniden koşuldu**.
>
> 🔴 **BU TURUN DERSİ, ve bir öncekinin ikizi:** geçen tur kural *"bir testin sorusu
> 'geçiyor mu' değil, 'koruduğu şey bozulsa kırmızıya döner mi'"* idi. Eksik olan
> ikizi bu turda ödendi: **bir YORUM yazdığında, yorumun İDDİA ETTİĞİ şeyi de
> yanlışlamaya çalış.** B2 ve B3 tam olarak böyle bulundu — ikisi de **üretim kodu
> mutasyonsuz**, yalnız cümlenin kendisi sınanarak.
>
> **İkinci güvenlik denetimi turu (2026-08-21, YENİ bir `tappa-security-auditor` —
> VERDICT: RED, bir YÜKSEK + iki ORTA + iki DÜŞÜK; hepsi kapatıldı).** Denetçi **33
> mutasyon** (31 kırmızı), **5 sonda testi** (`-race`) ve iki hedefli ölçüm koştu.
> **Kanıtıyla temiz bulunan geniş taban:** R1/R2 sıfır · R3 hiç SQL yok · **R4 tam
> kapalı** (`perUID` UID'nin var olduğu ilk anda, INSERT'ten **önce**, tek `st.mu`
> altında alınıyor; turu bitirmeden bırakan yol **yok**) · R6 sıra hem tabloda hem
> çalışma-zamanı guard'ında · R7 sızıntı temiz · **`Close`'un altıncı deliği ARANDI,
> BULUNAMADI** (**24 goroutine × 15 tur**, `-race`: `rounds=135 aborted=30 errs=225
> **residue=0**`) · M64'ün meşruiyeti bağımsız doğrulandı · `apdu.go` **gerçekten
> yalnız yorum**.
>
> 🔴 **F1 (YÜKSEK) — UID-İKAME KAPISI, ÖNLEMEK İÇİN VAR OLDUĞU HASARIN SONRASINA
> KONMUŞTU. Orkestratör kararı: kapı `index 6`'ya taşındı.** Kendi ölçümüm
> denetçininkini doğruladı: yalancı röle senaryosunda çip **`8D` (`WriteData`) ve
> `C4` (`ChangeKey`) komutlarını kabul etmiş** oluyordu
> (`A460AFAF71AF8DC451`), yani **kendi UID'siyle hiçbir satırda görünmeyen bir plaket
> anahtarı** taşıyordu — §5.2'nin 🔴 kalıcı kayıp modu, **kapının engellemek için var
> olduğu modun ta kendisi**. 🔴 **Ve yerleşimin TEK gerekçesi AYNI TURDA geri
> çekilmişti** (*"`K_0x01` bizimken MAC üretilemez"*): gerekçe düştü, yerleşim kaldı.
> **Tespit gücü değişmiyor ve bu ölçülebilir:** §5.1'e göre adım 5–8 **tek bir
> anahtar-0 oturumunda** koşar (adım 6 yeniden kimlik doğrulamaz; veri sayfası
> §10.6.1), yani yanıt MAC'i **her iki yerleşimde de** halka açık fabrika anahtarı
> 0'dan türer — risk 7'nin saldırganı ikisini de forge eder. **Kazanç tek taraflı:**
> uyumsuzluk artık yalnız **hayalet envanter satırı** bırakıyor. **Ek alışveriş yok**
> (`CommMode.Full`, adım 4'ten itibaren kullanılabilir). ADR 0017 **§5.1'e `4b` olarak
> yazıldı** ve **§6 md. 12 tadil edildi**. 🔴 **Ve ilgili test zararlı sırayı
> "beklenen" diye çiviliyordu** — çipin durumu hakkında **hiçbir iddiası yoktu**;
> artık uyumsuzluk anında **çipin yazılmamış, anahtarının fabrika değerinde ve hiçbir
> geri döndürülemez komutun geçmemiş** olduğunu ölçüyor (M67 kırmızı).
>
> 🔴 **F2 (ORTA) — *"çip hurdadır, anahtarı kurtarılamaz"* ÖLÇÜLEREK YANLIŞLANDI, ve
> bu cümle HATA MESAJINDA operatöre söyleniyordu.** Kendi ölçümüm: hayalet satırdaki
> sarmalın AAD'si **RowUID**'dir ve RowUID **halka açıktır** (mesajın kendisi
> yazdırıyor) → `sun.Unwrap(kek, RowUID, ref)` **açılıyor** ve çıkan anahtar
> **çiptekinin aynısı**; anahtar 0 da hâlâ fabrika değerinde. Yani plaket
> **kurtarılabilir** — ve eski cümle operatöre **gerçek bir Tappa plaket anahtarı
> taşıyan** bir çipi çöpe attırırdı. Doğru kurtarma yolu yazıldı; hata mesajı artık
> *"satırı elle emekliye ayır — **SAKIN SİLME**, anahtarın tek kopyası onda"* diyor.
>
> 🔴 **F3 (ORTA) — `Progress.Done`'ın güvenlik cümlesi, kodun ÜRETEMEYECEĞİ bir zincir
> anlatıyordu.** *"Yeniden koşturmak `ChangeKey`'i fabrika anahtarıyla çağırır ve
> düşer"* diyordu; ölçtüm: ikinci tur **dört alışveriş önce**, satır INSERT'inde
> ölüyor — *"duplicate key value violates unique constraint"* (`tags.uid` **PRIMARY
> KEY**, ve `InsertUnassigned`'ın **kendi sözleşmesi** üzerine yazmayı yasaklıyor).
> **Sonuç doğruydu, gerekçe ölçülmemişti** — ve fark operasyoneldir: operatör bir
> **çip arızası** değil, **bayat envanter gibi görünen bir DB hatası** görüyor, ve
> akla gelen ilk düzeltme **SATIRI SİLMEK** — ki o satır plaket anahtarının **tek
> kopyasını** tutuyor. Gerekçe ölçümle değiştirildi ve **"sakın silme"** uyarısı
> eklendi.
>
> **İki DÜŞÜK sertleştirme:** **F4** — `expireLocked`'ın *"her yazar buradan geçer"*
> iddiası hâlâ **nesirdi**; `retireCallSites` için mekanikleştirdiğim şey buna
> yapılmamıştı. `deadlineWriters` + `go/ast` ratchet eklendi
> (`TestSession_TheDeadlineHasExactlyOneRawWriter`, ham atama **1**), M68 kırmızı.
> · **F5** — **halkanın hangi kilit altında olduğu hiçbir yerde yazmıyordu**, ve
> bariz tahmin **yanlış**: halka `st.mu`'nun değil, **`s.busy`'yi tutan
> goroutine'in**. Denetçi `st.mu` **altında** peek ederek gerçek bir `-race`
> üretti. Bugün üretimde erişilemez, ama **B2c-2'nin sağlık yüzeyi tam o deseni
> kullanacak** (`armed()` test yardımcısı zaten kullanıyor) — cümle `keyring`'in
> doküman yorumuna yazıldı.
>
> ⚠️ **VE BU TURDA BİR ARAÇ KAZASI OLDU, KAYDEDİLMELİ:** paylaşılan scratchpad'deki
> `mutate.py` **bir denetçinin kendi script'i tarafından üzerine yazılmış**, ve o
> script **kendi yedeğinden geri yükleyerek F1 düzeltmemi (üretim kodu) geri aldı**.
> `go test` ile yakalandı (üç test kırmızı), F1 yeniden uygulandı ve doğrulandı; kendi
> script'im artık **`scratchpad/builder-mut/` altında izole**. Ders: paylaşılan
> scratchpad'de sabit adlı bir script, **başka bir ajanın geri yükleme yedeğiyle
> birlikte**, üretim kodunu sessizce geri alabilir.
>
> 🔴 **ALTINCI ARDIŞIK TUR: en ağır bulguların ikisi (F2, F3) yine BENİM bu turdaki
> yeni cümlelerimdi**, ve ikisi de **üretim kodu mutasyonsuzken**, yalnız **yorumun
> iddiası sondalanarak** bulundu — geçen tur yazdığım ikiz kuralın ta kendisi. Kural
> çalışıyor; eksik olan **onu kendi yeni cümlelerime uygulamak**.
>
> **Altıncı denetim turu (2026-08-21, ALTINCI ayrı üçüncü göz — VERDICT: RED, iki
> bloklayan + sekiz bloklamayan; hepsi kapatıldı).** 🔴 **DURMA KURALININ İŞARETİ:
> bulgular METNE döndü** — iki bloklayanın **ikisi de nesir**, sekiz bloklamayanın
> **yedisi** nesir/atıf. **Üretim yolunda yeni kusur yok; üst üste üçüncü denetim.**
>
> ✅ **VE SCRATCHPAD KAZASI DENETLENDİ: ÜÇÜNCÜ BİR SESSİZ GERİ ALMA YOK.** Denetçi
> tur 1–7'nin **21 adlandırılmış düzeltmesini** tek tek ölçtü — hepsi yerinde.
> **F1 bağımsız doğrulandı ve dayanağı PDF'ten ölçüldü** (§10.6.1 birebir
> *"Authentication with application key number 0 **is required** to change the key"*
> → adım 5–8 gerçekten tek anahtar-0 oturumunu paylaşıyor, yani *"tespit gücü
> değişmiyor"* gerekçesi **ayakta**). **12 mutasyon, 12 kırmızı**; kapıyı **index
> 8'e** de **index 7'ye** de geri taşımak **kırmızı**.
>
> 🔴 **B1 + B2 — VE İKİSİ DE AYNI ALT SINIF: BİR DÜZELTMENİN ÇÜRÜTTÜĞÜ, AMA YANINDA
> BIRAKILMIŞ KOMŞU CÜMLE.** B1: F2'nin geri çektiği *"çipi hurdaya at"* talimatı
> **`driver_test.go`'da iki yerde** hâlâ sevk ediliyordu — biri bir **`t.Fatalf`
> mesajı**, yani tam olarak birinin **karar verirken okuduğu** yer — ve F1 onu **iki
> kat** yanlış yapmıştı (artık hiçbir çip kişiselleştirilmiyor). Doğru ifadeyi aynı
> dosyada, 300 satır yukarıda, **aynı turda** yazmıştım. B2: `RelayMismatchError`'ın
> **doküman başlığı**, F1 öncesi sonucu (*"the chip **now holds** a plaque key … 
> permanent plaque loss … Detecting it does not undo it"*) **OLGU olarak** iddia
> etmeye devam ediyordu — üstelik kendini *"**named rather than softened**"* diye ilan
> ederek, ki bu bayat bir iddiayı **ölçülmüş** gibi okutan şeklin ta kendisi.
> Niteleyici yalnız **üç paragraf aşağıda** yaşıyordu. Ve operatöre görünen `Error()`
> bunu miras almıştı: *"it holds the **only copy of the key**"* — **eylem doğru,
> gerekçe yanlış**; satırı korumanın gerçek sebebi §5.2'nin *"sessiz temizlik
> yoktur"*u ve envanter izidir. Üçü de düzeltildi.
>
> 🔴 **VE BU TURUN OPERASYONEL KURALI, denetçinin adlandırdığı hâliyle:** bir düzeltme
> yaptığında `grep -rn`'i **düzelttiğin cümleye** değil **düzeltmenin ÇÜRÜTTÜĞÜ
> İDDİAYA** uygula. Bu turda taranan iddialar ve isabetleri: *"scrap"* (5) ·
> *"chip now holds"* (2) · *"steps 5 and 6 ran"* (1) · *"only copy of the key"* (1) ·
> *"nine steps/numbered"* (2) · *"four exchanges"* (1) ·
> *"Detecting it does not undo it"* (1). Hepsi ya düzeltildi ya geri-çekme alıntısına
> dönüştü.
>
> **Sekiz bloklamayan:** **N1** — `DefaultTTL` **yanlış testi** adlandırıyordu
> (*"drives all three"* diyen test üçünün **hiçbirini** sürmüyor); 🔴 **ve mekanik
> atıf kontrolü bunu GÖREMİYOR** — adı geçen 14 testin 14'ü de **var**, yani ratchet
> *"var mı"*yı ölçüyor, *"doğru mu"*yu değil. · **N2** — **altıncı kapalı sayım**
> (*"asserts **twice over**"*, testin kendi başlığı *"WHY **THREE**"* diyor). ·
> **N3** — alışveriş mesafesi bir fazla (*"four"* → **three**); aynı ifade 100 satır
> yukarıda **doğru**. · **N4** — 🔴 **F4 ratchet'i sayımı KAPATMIYORDU, ve bu
> ölçüldü:** deadline'ı **UZATAN** ikinci bir yazım **composite literal** olarak
> eklendiğinde test **yeşil** kalıyordu (`ast` yalnız `AssignStmt` eşliyordu);
> yakalayan şey **başka bir ratchet**ti. `KeyValueExpr` eklendi (**kendi mutasyonumla
> doğruladım: 3 ≠ 2 ile kırmızı**), ve **ne ölçtüğü/ne ölçmediği** yanına yazıldı. ·
> **N5** — iki envanterin doküman yorumları bitişikti, gerekçe yanlış olana
> bağlanıyordu; ayrıldı. · **N6** — `adr:` alanı, **izlemek için var olduğu belgeden**
> kaymıştı (ADR §5.1 artık `4b` diyor); alan ve iki *"nine steps"* cümlesi
> hizalandı. · **N7** — 🔴 **sahtede BEŞİNCİ boşluk adayı: denetçi belgeden
> ÇÖZEMEDİ, ben de çözemedim** — sahte **her** `ChangeKey`'den sonra oturumu ayakta
> bırakıyor, **anahtar 0 dahil**; §10.6.1 ve Tablo 63/64/65 kimlik doğrulanan anahtar
> değişince oturumun yaşayıp yaşamadığı hakkında **hiçbir şey söylemiyor**, ve
> `internal/sun/changekey.go` **ters tahmin** yapıyor (*"CASE 2 ENDS THE SESSION"*).
> **"Doğrulanamadı" olarak kaydedildi, "temiz" olarak DEĞİL**, ve **adım 8'in turuna
> devredildi**. · **N8** — `cmd/tappa/constanttime_test.go`'daki bayat *"14/9"* çifti
> güncel gibi okunuyordu; **tarihlendi** ⚠️ **kapsam dışı, işaretli**.
>
> ⚠️ **Bu turda kendi betiğim `scratchpad/builder-mut/` altında izole tutuldu ve beş
> üretim dosyası tur başında/sonunda md5 ile karşılaştırıldı** — beşi de değişti
> (bu tur beşini de düzenledim), yani sessiz geri alma yok.
>
> **Yedinci denetim turu (2026-08-21, YEDİNCİ ayrı üçüncü göz — VERDICT: RED, dört
> bloklayan + altı bloklamayan; hepsi kapatıldı).** 🔴 **VE DURMA KURALI HAKLI OLARAK
> TETİKLENMEDİ: sekiz turun kaçırdığı GERÇEK bir üretim kusuru bulundu**, yani tur
> 8'in *"iki nesir bloklayanı"* ikinci ardışık saf-nesir turu **değildi**. Zemin
> temiz: denetçi **36 düzeltmeyi** kendi komutuyla doğruladı → **dördüncü sessiz geri
> alma yok**; F1'in yerleşimi **index 7 VE index 8**, ikisi de kırmızı.
>
> 🔴 **B1 (ÜRETİM KUSURU) — `Step`, SON alışverişte context ölürse `Progress.Done`'ı
> DÜŞÜRÜYORDU.** Kendi sondamla yeniden ürettim (`checkout` `ctx.Err()`'i **saatten
> önce** okuduğu için, `Now()` içinde iptal eden bir `Clock`): `Done=false`,
> `err=context canceled` — ve **çip tam kişiselleştirilmiş** (anahtar `01` kurulmuş,
> NDEF 76 bayt, ayarlar 18 bayt), **`MarkEncoded` hiç çağrılmamış**. Bu,
> `Progress.Done`'ın **kendi 🔴 kuralını** yanlışlıyor (*"READ Done BEFORE READING THE
> ERROR … must NOT be re-run"*), ve aynı fonksiyon çiftindeki `finishLocked` **zaten
> doğru sıralıyordu** (`case done:` → `case ctxErr != nil:`). İki merdiven
> çelişiyordu; yanlış olan `Step`'inkiydi. **Sonucu F3'ün belgelediği zincirin ta
> kendisi:** yeniden koşan çağıran `duplicate key` alır → bayat envanter gibi okunur →
> bariz *"düzeltme"* **satırı silmek** → plaket anahtarının tek kopyası yok olur. Ve
> tetikleyici bir HTTP rölesi için **sıradan**: telefon son R-APDU'yu post eder, istek
> context'i ölür. Düzeltildi (`Done` artık `ctxErr`'den **önce**), ve **son
> alışverişte iptali süren bir test** yazıldı — mevcut iptal testi 10'un **4.**
> alışverişinde iptal ediyordu, bu pencere **hiç sürülmemişti** (M71 kırmızı).
>
> 🔴 **B2 — TURUN MERKEZÎ KABUL KRİTERİNDE (§6 md. 7 md. 3) NİTELEYİCİSİZ BİR TÜMEL.**
> *"Halkaya hiç ulaşmamış bir tampon orada düşer"* diyordu; denetçinin **iki
> mutasyonu da YEŞİL** kaldı (düz oturum anahtarını ve düz **plaket anahtarını** var
> olan bir bayt-dilimi alanına saklamak). Sebep yapısal: `armed()` `bufs`'ı
> `ring.filled()`'dan kuruyor ve **beş bilinen ada** çivili → **bilinen** bir tamponun
> kaydolmayı bırakmasını yakalar, **yeni** birini asla göremez. **Kapatılamayanı
> kapatmadım, ikiye böldüm:** *(a)* `Session`'a **yeni bir alan** eklenmesi artık
> mekanik (`TestSession_TheSessionFieldsAreInventoried`, 16 alan, M72 kırmızı) — ki
> adım 8'in ikinci plaket anahtarı **tam bu şekil**; *(b)* var olan bir alanın
> **yeniden kullanılması** hiçbir şey tarafından yakalanmıyor ve bu paketin
> yazabileceği hiçbir kaynak taraması onu yakalamaz → **SAYILMIŞ AÇIK olarak yazıldı**,
> durma kuralı 2'nin meşru saydığı biçimde.
>
> 🔴 **B3 — YEDİNCİ kapalı sayım: *"iki düz plaket anahtarı"*, üç niteleyicisiz kopya,
> gerçek sayı BİR.** Ölçtüm: `grep "ring.add(" internal/encode/*.go` (test dışı) **beş
> yer** veriyor ve `keyNameAppMaster` **hiç** geçmiyor — `keyInventory`'nin kendi
> yorumu bunu söylüyor, `armed()` **bir** plaket anahtarı çiviliyor. Kopyalardan biri
> **TTL tavanının gerekçesiydi**. Üçü de düzeltildi ve **ölçüm** taşıyıcı olanın
> yanına yazıldı. (Maruziyeti fazla söylüyordu, az değil — ama yazılı bir sayım.)
>
> 🔴 **B4 — F1'in çürüttüğü yerleşim, bu turun DÜZENLEDİĞİ dosyada hâlâ sevk
> ediliyordu.** `internal/sun/apdu.go` *"the session that exists **after ADR 0017 §5.1
> step 6**"* diyordu; tur o cümleyi düzenlemiş, yalnız **MAC iddiasını** daraltıp
> **yerleşim yarısını bırakmıştı**. ADR §6 md. 12 yerleşimi açıkça geri çekerken
> `apdu.go` onu çıplak ifade ediyordu — **örüntü 4**. Geri çekildi; `apdu.go` **hâlâ
> yalnız yorum** (`git diff -U0 | grep -v '^[-+]\s*//'` → **0 satır**).
>
> **Altı bloklamayan:** **N1** — tek ilişki için **iki mesafe** (*"four … and now
> three"*); *"dört"* `writedata`'ya olan mesafeydi ve **tur 8'in kendi N3 düzeltmesi**
> onu yanına bırakmıştı. · **N2** — `deadlineWriters` envanteri **birbiriyle çelişen**
> iki paragraf taşıyordu (*"sayılmadı"* / *"sayıldı"*); değer **2**, ikisi de sayılı,
> çelişen yarı silindi. · **N3** — *"a fourth test enumerates the state space **an
> entire round actually emits**"*: o test **bilerek tur KOŞMUYOR**, ve koşmamak b11
> düzeltmesinin **bütün içeriğiydi**; cümle iz çerçevesini geri takıyordu. · **N4** —
> `s.rowWritten` çivisiz bir guard'dı (ataması insert'in üstüne alınınca suite yeşil);
> bugün ulaşılamaz ama guard'ın **ilan edilmiş görevi** gelecekteki bir yeniden
> sıralamada ayakta kalmak, o yüzden çivilendi (M73 kırmızı). · **N5** — *"SEKİZ yol"*
> diyordu, gövdede **yedi** alt-test var (sekizinci komşu dosyada); sayı gövdeyle
> eşitlendi. · **N6** — 🔴 **kapsam dışıydı ama gerçek**: md. 12b'nin anmadığı
> **ÜÇÜNCÜ** uyumlu belge ifadesi — **Tablo 13, *"Default communication modes per
> file"***, dosya `02h` → **CommMode.Plain**. Kendi ölçümümle doğrulandı ve **hem
> ADR'ye hem koda** eklendi: belge *"bir gerilim"*in ima ettiğinden **daha tekdüze**.
>
> ---
>
> ## 🔴 KAPATILMAMIŞ SAYIM — B2c-2 / B3 / adım 8'e DEVREDİLENLER
>
> **Bu liste bir sonraki oturumun okuyacağı tek kayıttır. Hiçbiri kapatılmadı; hepsi
> adıyla devrediliyor.**
>
> 1. **§6 md. 5** — anahtar 0'ın şeması. §5.1 **adım 8 SEVK EDİLMEDİ**; bedeli **ADR
>    0005 risk 8** ve *"anahtar 0 fabrikadayken plaket duvara çıkamaz"* pilot çizgisi.
> 2. **§6 md. 8** — `audit_log` izi: olay adı · aktör · hangi tenant. Üçü de kararsız,
>    bu yüzden **hiçbiri uydurulmadı**.
> 3. **§6 md. 10** — yetkilendirme kapısı. `Begin`'in `actor`'ı bir **maruziyet
>    sınırıdır, yetki değil**.
> 4. 🔴 **R5 BLOKLAYICI ŞART:** `tenant_id` porta **AÇIK PARAMETRE** olmalı; örtük
>    bırakılırsa §4.5'in *"kemer"*i tümüyle uygulamanın hafızasına kalır.
> 5. **§5.1 adım 9'un şema karşılığı yok** — `tags`'te *"encoded"* sütunu bulunmuyor
>    (00004+00013; `status` **`unassigned`** kalmak zorunda). `MarkEncoded` bugün
>    **tüketicinin ihtiyacını** beyan ediyor.
> 6. **§6 md. 11 (Q08)** — host kararlaşmadı; yanlış host'la encode = plaket değişimi.
> 7. **§6 md. 12b** — **adım 5'in CommMode'u ÖLÇÜLMEDİ**. Belge **üç** yerde
>    `CommMode.Plain` diyor (§8.2.3.3 · §8.2.3.5 · **Tablo 13**); sürücü **Full**
>    gönderiyor; **hiçbir yayımlanmış örnek** bu kombinasyonu kapsamıyor. Yedek:
>    §5.8.1 `ISOUpdateBinary`. **Bloklamıyor** (adım 5 her `ChangeKey`'den önce koşar).
> 8. 🔴 **ÇÖZÜLEMEDİ, TEMİZ DEĞİL — kimlik doğrulanan anahtar değişince oturum yaşıyor
>    mu?** Sahte çip **her** `ChangeKey`'den sonra oturumu ayakta bırakıyor, **anahtar
>    0 dahil**. §10.6.1 yalnız *"Authentication with application key number 0 is
>    required"* diyor; Tablo 63/64/65 oturumun kaderi hakkında **hiçbir şey**
>    söylemiyor; belge geneli sonlanma-dili taraması yalnız §9.1.9/§9.1.10'u döndürüyor
>    (ikisi de **komut hatası**). `internal/sun/changekey.go` **tersini varsayıyor**
>    (*"CASE 2 ENDS THE SESSION"*). **Adım 8'in turu bunu silikonla çözmeli.**
> 9. 🔴 **Sahtede DÖRDÜNCÜ aynı-şekil aday:** `ChangeFileSettings` başlığındaki
>    **`FileNo` DEĞERİ yok sayılıyor** (`c.fileSettingsBody` tek alan) — dosya
>    numarası ve ofset/uzunluk boşluklarıyla **aynı sınıf**. Bu katmandan ulaşılamaz
>    (`SDMFileSettings.FileNo` `internal/sun`'da kuruluyor), **`internal/sun` değiştiği
>    gün canlanır**.
>    ⚠️ **Ve aynı sahtenin BEŞİNCİ sınırı (9. denetim, N4):** Tablo 65'in ön koşulu bir
>    **`t.Fatalf`** ile modellenmiş — gerçek silikon **`AUTHENTICATION_ERROR AEh`
>    çerçevesi** döndürür **ve §9.1.10 gereği kimlik doğrulama durumunu düşürür**, ki
>    sürücünün `RequireStatus`'ının göreceği ve fail-closed yolunun yazıldığı şey odur.
>    Başlıktaki *"enforced"* **kontrol** için doğru, **davranış** için değil. Bugün
>    ulaşılamaz; **adım 8 / probe-2 turunda canlanır**.
>    ⚠️ **VE AYNI SEÇİM EN AZ BEŞ YERDE DAHA VAR** (10. denetim, N3): runt C-APDU ·
>    sınıf baytı · `Lc` uyuşmazlığı · gövdenin header+MAC'ten kısa olması ·
>    `WriteData` uzunluk uyuşmazlığı — gerçek silikon hepsinde **durum sözcüğü
>    çerçevesi** döndürür. 🔴 **Ama bunlar MATERYAL OLARAK DAHA ZAYIF ve öyle
>    yazılmalı:** uyumlu bir sürücünün **hiç üretmediği** çerçeveler, yani orada
>    `t.Fatalf` bir **davranış modeli değil bir assertion**'dır ve doğrudur. Tablo
>    65'inki farklıdır çünkü oraya **meşru bir gelecek yol** (probe 2 / adım 8) varır —
>    devredilen tek şekil odur.
>    ⚠️ **Ve aynı sahtenin ALTINCI sınırı (10. denetim, N7):** `Transceive` `t.Fatalf`
>    çağırıyor ve **test goroutine'i dışından** ulaşılabilir konumda (eşzamanlılık
>    testleri turları spawn edilen goroutine'lerden sürüyor); `testing` bunu geçersiz
>    sayar. **Bugün ateşlenmiyor** (her `Fatalf`, uyumlu bir sürücünün üretmediği bir
>    çerçeveyi koruyor) ve `go vet` bu şekli **görmüyor**. Ateşlerse bedeli **teşhis
>    kalitesidir, doğruluk değil**.
> 10. **`Close`'un kalan artığı:** **hiç dönmeyen** bir sahip (sonsuza kadar bloke bir
>     port çağrısı) halkasını canlı tutar; `CloseGrace` bunu **sınırlamıyor**.
> 11. **§5.3'ün üç sondası** — komut kurucuları `internal/sun`'da **var**, sürücüsü
>     yok; ayrı bir akış.
> 12. **M64 meşru hayatta kalanı** — Tablo 65 ön koşulu iki ölçütü çakıştırdığı için
>     hiçbir davranışsal test onları ayırt edemez; **adım 8 geldiğinde gerçek olur**.
> 13. 🔴 **Plaket anahtarının ve `RndA`'nın ÖNGÖRÜLEMEZLİĞİ — bu turda çivilendi, ama
>     sınırı yazılı.** 9. denetim ölçtü: `mintPlaqueKey`'i **sıralı bir sayaca**
>     çevirmek (ayrık ama tümüyle öngörülebilir) **üç kapıyı da** geçiyordu; testler
>     yalnız **AYRIKLIK** ölçüyordu, oysa hamiline oturum handle'ının **dağılım** testi
>     zaten vardı. `TestDriver_ThePlaqueKeyAndRndAAreUNPREDICTABLENotMerelyDistinct`
>     eklendi (64 örnek, bayt konumu başına ≥30 ayrık değer, **ikisi için de**).
>     ⚠️ **Ne kanıtladığı sınırlı ve yanında yazılı:** bir dağılım testi
>     *"öngörülemez"* **kanıtlamaz**; yalnız **bir SINIF** öngörülebilirliği (sayaç,
>     zaman damgası, PID) eler. İstatistiği iyi ama kriptografik olarak zayıf bir PRNG
>     **geçer**. Nihai dayanak `crypto/rand`'dır; onu tutan tek şey bu testin
>     **değişikliği fark etmesidir**.
> 14. 🔴 **§6 md. 7 md. 3'ün AÇIĞI — ARTIK YER SAYMIYOR, ve bu ifadenin KENDİSİ
>     düzeltmedir.**
>     **Kaydolmamış bir tamponu HİÇBİR ŞEY görmez.** Mekanikleşen **tek** şey
>     `Session`'a **YENİ ALAN** eklenmesidir (`sessionFields`) — ki ADR 0017 §5.1
>     **adım 8**'in ikinci plaket anahtarı tam bu şekildir. **Diğer HER dinlenme
>     yeri yakalanmıyor:** başka bir tipte yeni **ya da var olan** alan · paket
>     değişkeni · **closure yakalaması** · map değeri · ve **map ANAHTARI**.
>     🔴 **MAP ANAHTARI KATEGORİK OLARAK DAHA KÖTÜ ve ayrı yazılmalı:** anahtar
>     baytları bir **Go DİZESİNE** girer; bir dize **hiç sıfırlanamaz** —
>     `sun.Zero`'nun yazacağı bir şey yoktur, `retireLocked`'ın `delete`'i girdiyi
>     siler **ama baytları silmez**, süpürücü ve `Close` da ulaşamaz. O şekil için
>     garanti **uygulanmıyor değil, KALICI OLARAK yanlış**.
>     ⚠️ **NEDEN SAYMIYORUZ — asıl ders bu:** üç ardışık denetimin **üçü de** sayımın
>     kaçırdığı bir şekil daha ölçtü (başka tipte yeni alan → başka tipte **var olan**
>     alan/map anahtarı → **closure yakalaması**), ve her seferinde liste uzatıldı,
>     her seferinde bir sonraki denetim uzatmayı da **eksik** buldu. **Saklanma
>     yerleri listesi tamamlanamaz**; yer saymak, doğduğu anda yanlış olan bir sayı
>     üretir. Yerden bağımsız ifade **eksik olamaz**, çünkü sayılacak bir şey
>     bırakmaz. Durma kuralı 2 (*"sayılmış açık, kapatıldığı iddia edilenden
>     güvenlidir"*) yalnız **sayım doğruyken** geçerlidir; eksik sayım, sayım
>     kılığında bir iddiadır.
> 15. 🔴 **`deploy/README.md` → *"Anahtar hijyeni"* md. 6'nın DÜŞÜRDÜĞÜ MEKANİK SINIR
>     YERİNE KONMADI — sayılıyor, kapatılmadı.** ADR 0017 §6 **md. 14** bunu
>     *"sayılmış kayıp"* diye kaydedip *"**turun 2'si** mekanik bir şeyle
>     değiştirmeli"* demişti; **turun 2 bu görevdir, bitti, ve değiştirmedi.**
>     Eski sınır *"yalnız sarmalı blob **çıktıya** çıkar"*tı — §4.7'nin **en dar**
>     hâli; yeni hâl yalnız **kalıcılaşanı** bağlıyor.
>     **Ölçüldü (2026-08-21):** süreçten **çıkanı** bağlayan **genel** bir kapı
>     **yok**. Bugün var olan üçü **adlandırılmış kanalları** bağlıyor, iddiayı
>     değil: `internal/encode` **hiç logger içermiyor** (kaynak okuyan test) ·
>     **hata mesajları** anahtar baytı taşımıyor (test) · `redline-check.sh` **R7**
>     gömülü anahtar dosyası ve sır taşıyan log çağrısı arıyor. Paket bugün
>     **hiçbir şey yazmıyor** (dosya/stdout yazıcısı **sıfır isabet**) — ama bu
>     **bugünkü kodun özelliğidir, bir kapı değil**, ve B2c-2 bir **HTTP uç noktası**
>     eklediğinde tam olarak o yüzey doğacak.
>     ⚠️ **md. 14'ün ÖTEKİ yarısı (md. 1) KAPANDI** — TTL + `defer` + süpürücü +
>     tek çıkış + yerden bağımsız garanti **sevk edildi**; ADR'de ve runbook'ta
>     **yerinde** kapatıldı.
> 16. 🔴 **§6 md. 1 — HİÇBİR ÇİP ENCODE EDİLMEDİ.** Bu turun tamamı belge okuması,
>     sahte çip ve mutasyondur. **FAZ B3 silikonu getirene kadar hiçbir şey
>     kanıtlanmış sayılmaz.**
>
> **Sekizinci denetim turu (2026-08-21, SEKİZİNCİ ayrı üçüncü göz — VERDICT: RED, dört
> bloklayan + beş bloklamayan; hepsi kapatıldı).** Denetçi üretim yolunu satır satır
> sondaladı (16 fonksiyon, 7 mutasyon, 5 çürütülmüş hipotez) ve sonucu yazdı: ***"Sevk
> edilen baytlarda YENİ bir sessiz-bozulma kusuru BULAMADIM."*** **24 düzeltme**
> doğrulandı → **beşinci geri alma yok**. Kalan dördün biri **kapı boşluğu**, üçü
> **sayım/nesir**.
>
> 🔴 **B1 — PLAKET ANAHTARI RASTGELE OLMAYI BIRAKABİLİYORDU VE ÜÇ KAPI DA YEŞİLDİ; TUR
> 1'İN B1'İ YALNIZ YARISI KAPATILMIŞTI.** Kendi sondamla yeniden ürettim:
> `mintPlaqueKey`'i **sıralı bir sayaca** çevirmek — her tur ayrık, ama tümüyle
> öngörülebilir — `go test -race` · `go vet` · `redline-check` **üçünü de** geçiyordu.
> Sebep: yazdığım iki tazelik testi de **AYRIKLIK** ölçüyordu, **ÖNGÖRÜLEMEZLİK**
> değil. Aynı mutasyon `RndA`'da da yeşildi. **Sonuç ADR 0003 md. 3'ün tam tersi:**
> tek bir plaketin anahtarını gören taraf **tüm filoyu sayar**. 🔴 **Ve emsal elimdeydi
> ve daha az değerli bir sır için yazılmıştı:** hamiline oturum handle'ının **dağılım**
> testi vardı, **AES-128 plaket anahtarınınki yoktu**. Aynı şekil ikisine de yazıldı
> (64 örnek, bayt konumu başına ≥30 ayrık değer); M74 ve M75 **kırmızı**. ⚠️ **Ne
> kanıtladığı yanına yazıldı:** bir dağılım testi *"öngörülemez"* kanıtlamaz, yalnız
> **bir sınıf** öngörülebilirliği eler.
>
> 🔴 **B2 — SEKİZİNCİ kapalı sayım, ve YEDİNCİYİ KAPATAN cümlenin içinde.** Raporum
> *"üç kopya, üçü de düzeltildi"* demişti; koddaki geri-çekme bloğu *"FOUR PLACES"*
> diyordu — **iki sayı zaten uyuşmuyordu**. Ölçüm: `grep -rn "two plain plaque"` →
> **7 isabet**, **6'sı niteleyicisiz**, biri **üretim kodunda**, **ikisi `t.Fatalf`
> mesajında**. Hepsi düzeltildi; sweep sonrası **1 isabet** kaldı ve o
> `keyInventory`'nin **gelecek zamanlı**, meşru olanı. Bu, tur 8'in B2 dersinin birebir
> tekrarıydı, bu kez altı kopyayla.
>
> 🔴 **B3 — F1/F2'nin çürüttüğü İKİ YARI, ADR'nin KENDİSİNDE emir kipinde duruyordu.**
> §6 md. 12'nin *"Gerçek çare — B2c'nin işi: **adım 6'dan sonra** … yanıt MAC'i röle
> tarafından **üretilemez**"* cümlesi hem **zararlı ölçülen yerleşimi** hem **geri
> çekilen MAC iddiasını** taşıyordu; alttaki blok yalnız MAC'i, 40 satır yukarıdaki
> blok ise *"yukarıdaki cümle"* diyerek **bu cümleyi kapsamadan** yerleşimi geri
> çekiyordu. Raporum bunu *"ADR açıkça geri çekiyor"* diye yazmıştı — **ölçüldü, ADR
> onu çıplak ifade etmeye devam ediyordu**. İkisi de **yerinde** geri çekildi.
>
> 🔴 **B4 — B2'nin *"ikiye böldüm"*ü uzayı KAPSAMIYORDU.** Denetçi **üçüncü** bir şekil
> ölçtü: **`Session` DIŞINDAKİ** bir tipe yeni alan (`Store.stash []byte` + plaket
> anahtarının kopyası) → **suite yeşil**, ve sayılmış açığın **metni o şekli tarif bile
> etmiyordu**. Sayım **üçe** çıkarıldı ve gerekçesi yazıldı: durma kuralı 2 (*"sayılmış
> açık, kapatıldığı iddia edilenden güvenlidir"*) yalnız **sayım doğruyken** geçerli;
> **eksik sayım, sayım kılığında bir iddiadır**.
>
> **Beş bloklamayan:** **N1** — 🔴 B1'in (ctxErr/Done) dayandığı *"iki merdiven
> çelişiyordu"* **emsali davranışsal olarak BOŞ**: `finishLocked`'ın iki `case`'ini
> takas etmek suite'i **yeşil** bırakıyor, çünkü **ikisinin gövdesi de aynı tek
> ifade**. Düzeltme doğru (M71 kırmızı), **gerekçe ölçülmemişti** — doğru sonucu
> ölçülmemiş bir nedenle desteklemek, bu paketin tekrar tekrar bulduğu sınıfın
> kendisi. · **N2** — TTL tavanının taşıyıcı cümlesi **15 satırlık bir blokla ikiye
> bölünmüştü** ve okunamıyordu; blok cümlenin **sonrasına** alındı. · **N3** —
> `sessionFields` kapısı **başarısızken** *"hepsi envanterde"* basıyordu (`t.Logf`,
> `t.Errorf`'tan sonra koşulsuz); artık `t.Failed()` ile korumalı. · **N4** — sahte çip
> Tablo 65'i **`t.Fatalf`** ile modelliyor, oysa gerçek silikon **`AEh` çerçevesi**
> döndürür ve §9.1.10 gereği kimlik doğrulamayı düşürür; **devir listesi md. 9'a
> yazıldı**. · **N5** — kartın kendi B2b kaydındaki aynı çürütülmüş çift **tarihsel
> diye işaretlendi** (kapsam dışı).
>
> 🔴 **DEVİR LİSTESİ 16 MADDEDİR:** denetçi 14'ün 14'ünü de doğru buldu ve iki
> madde eklenmesini istedi — **15** (öngörülemezlik çivisi ve sınırı) ve **16** (§6
> md. 7 md. 3'ün üçüncü şekli), artı **md. 9'a N4**.
>
> **Dokuzuncu denetim turu (2026-08-21, DOKUZUNCU ayrı üçüncü göz — VERDICT: RED, iki
> bloklayan + üç bloklamayan; hepsi kapatıldı).** İkisi de **sayım/kanıt** sınıfında;
> denetçi de bir öncekiyle aynı sonuca vardı — ***"sevk edilen üretim baytlarında yeni
> bir sessiz-bozulma kusuru bulamadım"*** — ve **nasıl aradığını** yazdı. **B1'in
> niteleyicisi bağımsız doğrulandı:** `crypto/rand → math/rand(seed 42)` mutasyonu
> **yeşil** kalıyor, yani *"iyi istatistikli zayıf bir PRNG geçer"* cümlesi **testin
> gerçekten taşıdığı** sınırdır; elediği sayaç sınıfı da gerçek (M74/M75).
>
> 🔴 **BLOKLAYAN 1 — SAYILMIŞ AÇIK YİNE EKSİKTİ: dördüncü ve beşinci şekil.** Kendi
> sondamla ikisini de yeniden ürettim: *(4)* plaket anahtarının baytlarını **başka bir
> tipin VAR OLAN alanına** — `st.perUID`'e, **Go DİZESİ map anahtarı** olarak — koymak
> → **suite yeşil**; *(5)* `append` + **closure yakalaması** ile bir goroutine'de
> tutmak → **`-race` yeşil**. Sayımın **(b)**'si *"on `Session`"* ile, **(c)**'si
> *"**NEW** field on another type"* ile sınırlıydı; **hiçbiri bu ikisini tarif
> etmiyordu** — tur 10'un B4'ünde (c)'yi eklerken kullanılan argümanın **aynısı**.
> 🔴 **Ve map anahtarı diğerlerinden KATEGORİK olarak farklı:** anahtar baytları bir
> **Go dizesine** girer, dize **hiç sıfırlanamaz** — `sun.Zero`'nun yazacağı bir şey
> yok, `delete` baytları silmiyor, süpürücü ve `Close` ulaşamıyor. O şekil için §6
> md. 7 md. 3 **uygulanmıyor değil, KALICI OLARAK yanlış**.
> 🔴 **ÇÖZÜM (denetçinin verdiği ve uyguladığım): YER SAYMAYI BIRAK.** İfade artık
> yerden bağımsız — *"kaydolmamış bir tamponu **hiçbir şey** görmez; mekanikleşen tek
> şey `Session`'a **yeni alan** eklenmesidir"* — ve **map anahtarı adıyla anılıyor**.
> **Bu, kapalı-sayım sınıfını bu madde için KAPATIR:** sayılacak yer listesi kalmaz,
> dolayısıyla eksik sayılacak bir şey de kalmaz. **Üç ardışık denetimin üçü de** listeye
> bir şekil daha ekletmişti; **saklanma yerleri listesi tamamlanamaz.** Hem koda hem
> devir listesi **md. 15**'e (eski 16) bu hâliyle yazıldı, ve M76/M77 mutasyon
> tablosunda **kalıcı** — açık **görünür** kalsın, mekanikleşirse **kırmızıya dönsün**.
>
> 🔴 **BLOKLAYAN 2 — N1'İN YERİNE KOYDUĞUM GEREKÇE DE ÖLÇÜLMEMİŞTİ, VE YANLIŞTI.**
> *"The **only** load-bearing order in that switch is `case done:` against the deadline
> case"* — kendi ölçümüm: `case done:`'ı deadline case'inin **altına** almak da
> **yeşil**. Yapısal sebep: `finishLocked`'ın **tam bir** çağrı yeri var, dönüşü
> `lateErr`'e bağlı, ve `Step` `p.Done` doğruyken `markEncoded` ile dönüp **`lateErr`'i
> hiç okumuyor** → dönüş **gözlenemez**, ve üç dal da **aynı tek ifadeyi** çalıştırıyor.
> 🔴 **İroni kayda değer:** *"doğru sonucu ölçülmemiş bir nedenle desteklemek bu
> paketin tekrar bulduğu sınıfın kendisi"* diyen blok, bir ölçülmemiş iddiayı **başka
> bir ölçülmemiş iddiayla** değiştirdi. **Çözüm: emsal aramayı bırak.** Artık
> *"o switch'te **yük taşıyan bir sıra yok**"* yazılı ve **ölçülmüş** (M78 kalıcı
> hayatta kalan olarak tabloda); düzeltme `Progress.Done`'ın **kendi kuralına** ve
> M71'e dayanıyor — **bir düzeltme gerekçesi için başka bir yerde simetriye ihtiyaç
> duymaz**.
>
> **Üç bloklamayan:** **N1** — devir listesi **16 maddeye 17 numara** veriyordu (md. 14
> *"bkz. madde 13"* diyen **içeriksiz bir işaretçiydi**); silindi, yeniden numaralandı,
> **liste 16 maddedir**. · **N2** — süpürme sayım **birebir üretilmiyordu**, çünkü
> **cümlenin kendisi arama dizesini içeriyor**; iddia yerden bağımsız hâle getirildi ve
> *"sonucu kendini içeren bir grep, kodun ölçümü değildir"* yazıldı. · **N3** — sahtenin
> `t.Fatalf` modellemesi md. 9'un dediğinden **geniş** (en az beş yer daha); md. 9
> genişletildi **ve materyal farkı yazıldı**: ötekiler **uyumlu bir sürücünün hiç
> üretmediği** çerçeveler, yani orada `t.Fatalf` bir **assertion**'dır ve doğrudur —
> Tablo 65'inki farklı, çünkü oraya **meşru bir gelecek yol** varır.
>
> **Onuncu denetim turu (2026-08-21, ONUNCU ayrı üçüncü göz — VERDICT: RED, iki
> bloklayan + yedi bloklamayan; hepsi kapatıldı). İKİSİ DE METİN.**
>
> ✅ **ÜÇÜNCÜ BAĞIMSIZ DENETÇİ, FARKLI YOLLARDAN, AYNI SONUÇ:** *"sevk edilen baytlarda
> yeni bir güvenlik ya da doğruluk kusuru **BULAMADIM**"* (63 mutasyon, 47 kırmızı;
> `keyring`, `Store`'un yedi giriş noktası, on adımlık makine, `internal/sun` sınırında
> on argüman seçimi). ✅ **VE 11. TURUN BLOKLAYAN 1'İ TERMİNE ETTİ:** denetçi kendi
> **altıncı** şeklini üretti (`Session`'ın KENDİ var olan alanı) ve **yerden bağımsız
> ifade onu kapsıyor**; gizli bir yer listesi aradı ve **bulamadı**. **Sınıf kapandı.**
> ✅ **BLOKLAYAN 2 genişletildi:** `switch`'in **dört permütasyonu da yeşil**, ama
> **deadline kolunu silmek KIRMIZI** — *"hiçbir sıra yük taşımıyor"* artık bir örnekle
> değil **uzayın dördüyle** ölçülü, ve kolun kendisinin yük taşıdığı ayrıca çivili
> (M80).
>
> 🔴 **B1 — DEVİR LİSTESİNİN md. 10'u İÇERİKSİZ BİR İŞARETÇİYDİ, ve BU TURUN KENDİ
> CÜMLESİ ONA ATIF YAPIYORDU.** 11. turun N1'i tam bu kusurun **bir örneğini** silip
> **iki sıra ötedeki ikizini bırakmıştı** — *"düzeltmenin çürüttüğü iddiayı greple"*
> kuralının kendi düzeltmesine uygulanmaması. Ağırlaştıran: `session.go`'nun
> *"Handed on as **item 10** … in this same place-independent form"* cümlesi **boş bir
> girdiye** düşüyordu, yani B2c-2 oturumu kodun işaretçisini izlediğinde kartta
> **hiçbir şey** bulacaktı. Stub silindi, liste **15 maddeye** indi, kod **md. 14**'e
> işaret ediyor, ve **atıf komutla doğrulandı** — kod artık doğrulama komutunu
> **kendisi yayımlıyor**.
>
> 🔴 **B2 — KART *"§6 md. 7 KAPANDI"* DİYORDU; ADR'NİN KENDİSİ HÂLÂ *"AÇIK"* YAZIYORDU,
> VE ENVANTERİ SEVK EDİLEN KODUN İKİ YÖNDEN TERSİYDİ.** ADR §6 md. 7 birebir *"bugün
> **hiçbiri için** bir `Zero` kuralı yazılı değil"* diyordu ve oturum envanterini
> *"`KSesAuthENC`, `KSesAuthMAC`, **`TI`**, **`CmdCtr`**"* olarak sayıyordu — oysa
> `keyInventory` **`RndA`/`RndB`**'yi taşıyor ve kod `TI`/`CmdCtr`'ın **sır olmadığını**
> gerekçesiyle yazıyor: **iki fazla, iki eksik**. Aynı yanlış envanter **§4 tablosunda**
> da duruyordu. 🔴 *"Eski metin"* savunması geçmiyordu çünkü **ADR bu görevde
> düzenlendi** (§5.1 `4b`, md. 12, md. 12b) ve **ADR'nin kendi kuralı yerinde
> kapatmaktır**. **Somut bedeli:** adım 8'i yazacak oturum ADR'yi okuyacak ve orada
> `RndA`/`RndB` disiplinini **hiç görmeyecekti**. Md. 7 **ADR'nin kendi biçimiyle
> kapatıldı** (beş karar adıyla, envanter `keyInventory` ile **eşitlendi**,
> `TI`/`CmdCtr`'ın **neden dışarıda** olduğu yazıldı, **sayılmış açık** ve **map
> anahtarı** dahil), ve **§4 tablosundaki ikiz de düzeltildi** — `grep -rn` ile **iki
> kopya da** bulundu.
>
> **Yedi bloklamayan:** **N1** — yayımlanan `grep` **kendi cümlesini sayıyordu**;
> komut kaldırıldı ve 🔴 **yerine temiz bir komut KONMADI, çünkü yok**: bir sembolü
> tartışan dosya o sembol için greplenemez. · **N2** — *"all three arms"* **dört kollu**
> bir switch hakkındaydı; hangi üç olduğu ve dördüncünün ayrıca `return` ettiği
> yazıldı. · **N3** — **örüntü 2'nin altıncı vakası**: `allZero(bytesOf(0, 0))` boş
> dilim üzerinde **önemsizce true**, ve iddia ettiği tamponlar o fonksiyonda bir
> değişkene **bağlanmıyordu bile**. **Onarılmadı, SİLİNDİ** — özellik komşu testte
> gerçekten çivili. · **N4** — **dokuzuncu hayatta kalan** tabloya eklendi (M79) ve
> **kontrolüyle** birlikte (M80 kırmızı). · **N5** — dört *"ileriye dönük"* iddia
> çivisizdi; dördü de **fail-closed** olduğu için **LİMİT olarak yazıldılar**, ve
> ikisinde *"testin yanlış sebeple yeşil"* olduğu (mutantın `internal/sun`'ın kendi
> kapısına düşmesi) **adıyla** kaydedildi. · **N6** — `Progress`'in *"Command … nil
> when the round is over"* değişmezi çivisizdi; çivilendi (M81 kırmızı). · **N7** —
> sahtenin `t.Fatalf`'i **test goroutine'i dışından** ulaşılabilir; **bugün
> ateşlenmiyor**, `go vet` görmüyor, bedeli **teşhis kalitesi**; kodda ve devir
> listesi md. 9'da **işaretlendi**.
>
> **Orkestratörün commit öncesi doğrulaması (2026-08-21) — ÖRÜNTÜ 4, BU KEZ MD. 7'NİN
> KENDİ KAPANIŞI TARAFINDAN.** Her iki bloklayan düzeltmesi doğrulandı
> (`session.go:257` → **ITEM 14**, devir listesi **boşluksuz**, ADR md. 7
> `✅ KAPANDI`, `:297` envanteri düzeltilmiş) — **ama md. 7'yi kapatmam ADR'nin
> içinde komşuları çürüttü ve onları taramamıştım.** Kendi kuralımın
> (*"düzelttiğin cümleyi değil, düzeltmenin ÇÜRÜTTÜĞÜ İDDİAYI greple"*) **bu turdaki
> son uygulaması, ve tam da atladığım uygulama.**
>
> **Tarama (önce → sonra), üç dosyada:** `hâlâ AÇIK` **1 → 0** ·
> `yazılmamış bir yükümlülük` **1 → 0 canlı** (kalan tek isabet kapanışın **alıntısı**) ·
> `bugün karara bağlanmadı (§6 md. 7)` **1 → 0** ·
> `turun 2'sinin kabul kriterleridir` **1 → 0** ·
> `Turun 2'si bu ikisini … değiştirmeli` **1 → 0 canlı**. Kalan dokuz `turun 2`
> atfı **meşru** — md. 5 · md. 8 · md. 10 · md. 12 · XOR riski gibi **hâlâ açık**
> maddeler hakkında.
>
> ✅ **ÜÇ KOMŞU YERİNDE KAPANDI:** §3'ün *"o TTL bugün karara bağlanmadı"*ı → **90 sn** ·
> §4'ün *"TTL, eşzamanlılık sınırı ve iptali turun 2'sinin kabul kriterleridir"*i →
> **üçü de karara bağlandı** (90 sn · 1/3/64 · iptal bir **ÇIKIŞ YOLUDUR**) ·
> **md. 14'ün md. 1 yarısı** → *"o üçü §6 md. 7'de hâlâ AÇIK"* **çürüdü**; yerini alan
> şey artık **mekanizma** (TTL + `defer` + iki yollu süpürücü + tek çıkış + yerden
> bağımsız garanti). ⚠️ **Niteleyiciyle:** mekanizma **sevk edildi** ama
> `internal/encode` **hiçbir yerden import edilmiyor** (**0**), yani runbook'un
> *"araç yazıldığında; bugün yok"* parantezi **akışın bütünü için hâlâ doğrudur** —
> kapanan şey **yükümlülüğün nesir olması**.
>
> 🔴 **VE md. 14'ÜN ÖTEKİ YARISI KAPATILMADI, SAYILDI — devir listesi 15 → 16.**
> `deploy/README.md` md. 6'nın düşürdüğü sınır (*"yalnız sarmalı blob **çıktıya**
> çıkar"*) **yerine konmadı**. Ölçtüm: süreçten **çıkanı** bağlayan **genel** bir kapı
> **yok**; bugünkü üç mekanizma **adlandırılmış kanalları** bağlıyor (logger yok ·
> hata mesajı testi · R7), iddiayı değil. Paket bugün hiçbir şey **yazmıyor**
> (dosya/stdout yazıcısı **0**), ama o **bugünkü kodun özelliği, bir kapı değil** — ve
> B2c-2 bir **HTTP uç noktası** eklediğinde tam o yüzey doğar.
> ⚠️ *"Sayılmış bir açık, kapatıldığı iddia edilenden güvenlidir"* — bu yüzden
> **kapatılmadı, SAYILDI**, hem ADR md. 14'te hem runbook'ta hem devir listesinde.
>
> **Kapsam:** `deploy/README.md` **düzenlendi** (kart onu normatif olarak anıyor):
> md. 1'e kapanış ve niteleyicisi, md. 6'ya **sayılmış açık**. İkisi de **ölçümle**
> yazıldı, ikisi de yalnız nesir.
>
> **Kapsam dışı, işaretli:** ADR 0017 §6 md. 12'ye tarihli düzeltme bloğu ve
> `internal/sun/apdu.go`'nun `GetCardUIDCommand` yorumundaki ikiz cümlenin
> daraltılması. Gerekçe: ikisi de **ölçülerek yanlışlanmış bir güvenlik cümlesiydi**
> ve bu turun kodu tam o cümleye dayanıyor; düzeltmeden bırakmak, bu görevin
> "Ders 2"sinin tarif ettiği hatanın aynısını sevk etmek olurdu. **Bayt değişmedi,
> yalnız iki cümlenin niteleyicisi.**

**Tuzaklar.**
- **Yanlış host'la encode edilmiş plaket = sahada plaket değişimi.** SUN URL'si
  domaini taşır ve **Q08 hâlâ açık** (`tappa.mt`/`tappa.io` alınmadı). Encode
  edilen host **geri alınamaz**.
- Encode edilen satırın doğru durumu **`unassigned`**'dır (`active` değil):
  migration 00013'ün envanter modelinde plaket önce **yüklenir**, duvara sonra
  **bağlanır**. `active` yazmak, kutudaki plaketi hizmette göstermek demektir.

---

## M8-06 — KF St Julians pilotu

- **Bağımlılık:** M8-02 … M8-05 · Q13
- **Commit:** `docs: record pilot results`

**Amaç.** Tek şubede, 1 hafta, **BioTime ile paralel** çalıştırma.

> **PİLOT KAPISI (Q23) — hepsi ✓ olmadan pilot başlamaz.** Pilot gerçek
> çalışanların gerçek konum ve IP verisiyle koşacak:
> - [ ] GDPR Art. 13 çalışan aydınlatma metni yayında ve aktivasyonda gösteriliyor ([M5-02](m5-tap-akisi.md))
> - [ ] Gizlilik politikası, hizmet şartları, imprint yayında ([M7-01](m7-portal.md))
> - [ ] Tappa↔KF veri işleme sözleşmesi (DPA) imzalı ([M8-02](m8-deploy-pilot.md))
> - [ ] Saklama süresi ve silme/anonimleştirme akışı karara bağlı (Q13)
> - [ ] Telefon envanteri çıkarıldı ([M8-07](m8-deploy-pilot.md))
> - [ ] Üretim tenant'ı kuruldu ve doğrulandı ([M8-07](m8-deploy-pilot.md))
> - [ ] 🔴 **Duvara çıkan hiçbir plakette uygulama anahtarı 0 fabrika
>       varsayılanında DEĞİL** ([ADR 0017](../adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)
>       §5.0 · [ADR 0005](../adr/0005-kabul-edilen-riskler.md) risk 8)
>       — **madde 2026-08-20'de eklendi.**
>       **Ön koşulu bir şema kararıdır:** ikinci plaket anahtarının nerede
>       saklanacağı (ADR 0017 §6 md. 5; üç şık `deploy/README.md` → *"FAZ B'ye
>       devredilenler"* md. 9'da sayılı). O karar kapanmadan bu kutu
>       işaretlenemez.
>       ⚠️ **Bu kutunun bugün MEKANİK KARŞILIĞI YOK ve bunu bilerek yazıyoruz:**
>       `AssignTagToLocation`'ın WHERE'i yalnız `tenant_id` + `uid` +
>       `status = 'unassigned'` bakıyor; anahtar 0 durumunu okuyan bir sütun ya da
>       kontrol ağaçta **yok** (ölçüldü: `master_key`/`key0`/ikinci anahtar sütunu
>       için `internal`, `db`, `cmd` taramasında sıfır isabet). Yani bugün bu bir
>       **insan kontrolüdür**; mekanikleştirmek M8-05 turun 2'sinin ya da M8-06'nın
>       işidir.
>       **Neden pilot kapısında:** encode aracının inşası ve testi bunu beklemez
>       (tezgâhta ve kutuda sorun yok); duvara çıkmak bekler. Sinyalsiz bir risk
>       olduğu için (ADR 0005 risk 8 — repointlenen plaketin tap'i bize **hiç
>       ulaşmaz**) sonradan yakalanamaz.
>
> "BioTime'ın GDPR yükünden kurtulun" diye satılan bir ürünün kendi pilotunda
> aydınlatma metni olmadan çalışması, ürünün ana satış argümanını çürütür.

**Kabul kriterleri.**
- Pilot kapısındaki **yedi** madde de ✓ — **yedincisi 2026-08-20'de eklendi**
  (anahtar 0 fabrika varsayılanında değil; [ADR 0017](../adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)
  §5.0 · [ADR 0005](../adr/0005-kabul-edilen-riskler.md) risk 8). ⚠️ Bu satır
  **altı** diyordu ve kapı yediye çıktığı hâlde 24 satır aşağıda güncellenmemişti;
  o hâliyle pilot, anahtar 0 kutusu işaretlenmeden *"kapı tam"* ilan edilebilirdi.
- 15 kişilik kadro aktive edildi; her çalışanın practice tap'i tamamlandı.
- 1 hafta boyunca iki sistem paralel; günlük kayıt karşılaştırma tablosu.
- Uyuşmazlıkların her biri açıklandı (kim, ne zaman, neden) — açıklanamayan
  fark **kalmadı**.
- `flag` oranı ve nedenleri raporlandı — politika `sid` kırılımıyla (M3-07).
- **IP eşleşme oranı ölçüldü (Q14).** Kaç tap mekânın ağından geldi, kaçı mobil
  veriyle? Oran düşükse `proof of place` pratikte GPS'e indirgenmiş demektir;
  trust ağırlıkları (50/30) ve `base:gps-only-allow` varsayılanı buna göre
  yeniden değerlendirilir.
- **GPS-only tap oranı** ve WiFi adımını atlayan çalışan sayısı raporlandı.
- Çalışan geri bildirimi toplandı (ekran anlaşıldı mı, kaç saniye sürdü).
- Q13 (GDPR saklama/silme politikası) karara bağlandı ve yazıldı.
- Sonuç: iki firmaya tam yayılım kararı → BioTime kapatılır.

**Tuzaklar.**
- Pilot sırasında kod değiştirmek karşılaştırmayı bozar. Kritik hata dışında
  değişiklik pilot bitiminde.

---

## M8-07 — Üretim tenant kurulumu ve cihaz envanteri

- **Bağımlılık:** M7-03 · M8-02
- **Kırmızı çizgi:** §4.5 · §4.7
- **Commit:** `docs(ops): document production tenant setup`

**Amaç.** Pilotun üzerinde koşacağı **gerçek** tenant'ı kurmak ve sahadaki
telefonların ne yapabildiğini önceden bilmek.

**Neden.** Denetimde çıktı: M1-10 seed'i açıkça **sahte** (dokümantasyon IP'leri,
sahte AES anahtarları). M8-06 "15 kişilik kadro aktive edildi" diyor ama gerçek
KF tenant'ının hangi araçla, hangi adımlarla kurulacağı hiçbir kartta yoktu.

**Adımlar.**
1. KF tenant'ı M7-02 kayıt sihirbazından **gerçek veriyle** açılır (seed ile değil).
2. 9 lokasyon, gerçek statik IP'ler ve GPS koordinatları girilir; Rusty Bar
   `overnight = true`.
3. Her lokasyonun WiFi SSID'si lokasyon kaydına girilir ([M5-02](m5-tap-akisi.md)
   aktivasyon adımında çalışana gösterilecek).
4. 15 çalışan profili girilir; davetler gönderilir.
5. Plaketler encode edilir ([M8-05](m8-deploy-pilot.md)) ve UID'ler lokasyonlara bağlanır.
6. **Telefon envanteri:** kadroya tek soruluk anket — telefon modeli. Amaç:
   arka planda NFC okuyamayan cihazları (iPhone X ve öncesi) saymak.

**Kabul kriterleri.**
- Üretim tenant'ında **hiçbir** seed verisi yok; sahte AES anahtarı, dokümantasyon
  IP'si veya `91AC7E55…` tarzı örnek UID bulunmuyor.
- Statik IP'ler müşteriden **teyitli** (ISS yazısı veya test tap'i ile doğrulandı).
- NFC okuyamayan telefon oranı biliniyor. Yüksekse ([M5-08](m5-tap-akisi.md)):
  o çalışanlar kalıcı olarak QR yolunda demektir ve Q15 gereği IP zorunlu —
  WiFi adımı onlar için kritik, gerekirse tenant bazında politika gözden geçirilir.
- Gerçek bir plaketle, gerçek bir telefonla **en az bir uçtan uca tap** yapıldı
  ve kaydı panelde görüldü.
- Kurulum adımları runbook'a yazıldı — ikinci tenant (KM) aynı adımlarla açılacak.

**Tuzaklar.**
- Gerçek statik IP'leri ve müşteri verilerini **repoya yazma** (§4.7 ruhu;
  `tappa-seed` skill'i dokümantasyon aralıklarını şart koşuyor).
