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
> `go version -m` → `go1.26.6`. `make audit` **hâlâ kırmızı**, çünkü govulncheck
> geliştirici makinesinin toolchain'ini sayıyor.
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

**FAZ A — donanımsız yarı (bu tur).**

| # | Kriter | Durum |
|---|---|---|
| 1 | Encode ayarları yazılı: UID mirror + counter mirror açık, **MAC girdisi boş** (ADR 0003 md. 2), dosya okuma izni açık, **yazma izni anahtarla kilitli** | ✅ bugün yapılabilir — runbook'ta, ADR'den normatif türetildi |
| 2 | Geliştirme akışı: 10'luk NTAG 424 DNA paketi (~30 €) + **araç yolu** | ⚠️ **yarısı** — paket kullanıcıda **VAR**, yazıcı **YOK**. Runbook iki yolun **ŞEKLİNİ** ve **kısıtını** yazıyor; **maliyet ölçülmedi**, araç iddiaları **zayıf kanıt** diye etiketli. **Seçim FAZ B'de ve kullanıcınındır** (§1). |
| 3 | **En az bir fiziksel etiketle uçtan uca doğrulama** | 🔴 **donanıma bloke** → FAZ B |
| 4 | Anahtar teslimi ve döndürme prosedürü | ✅ bugün yapılabilir — runbook'ta; `retire + replace` ile KEK rotasyonu **ayrı tutuldu** |
| 5 | Anahtarlar repoya/sohbete/e-postaya yazılmadı; KEK ile sarmalı DB'de | ✅ bugün yapılabilir — **mekanizma olarak** yazıldı (`sun.Wrap`/`sun.Zero`, R7 kuralları, `seedkeys` emsali) |
| 6 | Plaket baskısı: A5, NFC + QR birlikte, kamera görüş alanına montaj notu | ✅ bugün yapılabilir — skill `tappa-brand` → "Plaket baskısı"na atıfla; QR'ın §5 sonucu yazıldı. ⚠️ **A5 skill'de YOK** (ölçüldü: `A5`/`148`/`210` sıfır isabet); kâğıt boyu **handoff.md**'den gelir, skill yerleşim/QR ölçüsü verir. |

**FAZ B — fiziksel çip gerektiren yarı (devir).** Sekiz maddelik yükümlülük
listesi runbook'un sonundadır: `deploy/README.md` → **"FAZ B'ye devredilenler"**.
Kriter 2'nin araç seçimi ve kriter 3'ün tamamı oraya aittir.
⚠️ **Listenin 1. maddesi M2-08 ile DARALDI ve sıra ayrıcalığını kaybetti.** Eskiden
*"`ctr` bayt sırası — diğer yedisinden önce koşulur"* diyordu; o gerekçe (*yanlış
tarafta duran kod her tap'i reddeder*) **decode** tarafı içindi ve decode tarafı
artık kapalı. Kalan iş **encode** tarafının doğrulanmasıdır: encode aracı sayacı
URL'ye gerçekten **MSB-first** yazıyor mu. Bu, kalan yedi maddeyle **aynı turda**
ölçülebilir; ayrıca `ctr` palindrom olmadığı için sinyal gürültüsüzdür.

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
>
> "BioTime'ın GDPR yükünden kurtulun" diye satılan bir ürünün kendi pilotunda
> aydınlatma metni olmadan çalışması, ürünün ana satış argümanını çürütür.

**Kabul kriterleri.**
- Pilot kapısındaki altı madde de ✓.
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
