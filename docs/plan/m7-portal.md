# M7 — Portal & signup

**Amaç.** Kendi kendine kayıt: pazarlama sitesi, 3 adımlı kayıt sihirbazı,
tenant provisioning. Handoff §9'daki `tappa-landing.html` ve `tappa-portal.html`
tasarımı **templ ile yeniden yazılır** (ADR 0001); tasarım dili korunur.

**Ana araç:** skill `tappa-brand`

---

## M7-01 — Landing sayfası

- **Bağımlılık:** M6-02 (bileşenler)
- **Commit:** `feat(web): add marketing landing page`

**Bölümler** (handoff §9): hero (canlı plaket + basılan adisyon animasyonu) ·
3 adım · parmak izi karşılaştırma tablosu · 4 kanıt · chain vs facility ·
fiyat (€1.50 + 3 ay ücretsiz) · FAQ · CTA → portal.

**Kabul kriterleri.**
- Slogan: *No app. No device. No fingerprints. Just tap.*
- Fontlar self-host; harici çağrı yok (GDPR).
- **Yasal sayfalar burada yayına girer (Q23):** gizlilik politikası, hizmet
  şartları, imprint/şirket künyesi, çerez bilgilendirmesi. Footer'dan erişilebilir.
  Denetimde çıktı: planda hiçbir yasal metin yoktu.
- Animasyon `prefers-reduced-motion` saygılı, sallanan/gradient efekt yok.
- Tek binary'ye gömülü (`web/embed.go`), ayrı statik host gerekmiyor.

> **Kart düzeltmesi (2026-08-13, M7-01 uygulaması sırasında).** Dört madde kart
> gerçekle çeliştiği ya da eksik kaldığı için burada kayda geçiyor.
>
> 1. **"Fontlar self-host" kriteri BU GÖREVDEN ÖNCE karşılanıyordu.** M5-04
>    `web/static/fonts/` altına altı woff2 koydu (92 KB, latin + latin-ext) ve
>    `input.css`'teki altı `@font-face` kendi origin'imize bakıyor. M7-01'in işi
>    kurmak değil **bozmamak ve ölçmek**ti: render edilen beş sayfada mutlak/
>    protokol-göreli referans **sıfır** (`TestMarketing_ReachesNoThirdParty`,
>    pozitif kontrolüyle birlikte).
> 2. **"CTA → portal" BUGÜN MÜMKÜN DEĞİL.** Sihirbaz M7-02'de; M7-01 tek başına
>    sevk edilince "Start free" düğmesinin gideceği yer yok. Sayfa bunu
>    **kelimeyle** söylüyor ve ölü bağlantı **basmıyor**;
>    `pages.LandingView.SignupHref` boş kaldığı sürece düğme render edilmiyor.
>    **M7-02'nin işi:** rotayı mount etmek **ve** `handler.Marketing.signupHref`'i
>    doldurmak — ikisi de yapılmazsa `TestMarketing_EveryInternalLinkResolves`
>    ya da `TestLanding_OffersNoLinkToAWizardThatIsNotMounted` kırmızıya döner.
> 3. **Kart `robots` meta'sından söz etmiyor ve bu bir tuzaktı.** `layout`'un ortak
>    `<head>`'i M7-01'e kadar HER sayfaya `noindex, nofollow` basıyordu — aktivasyon
>    linki bir tarayıcı botuna düşmesin diye doğru bir karar, ama bulunmak için var
>    olan bir sayfa için tam ters. Değer artık `layout.Robots` (kapalı sözcük
>    dağarcığı) ile veriliyor: landing `index, follow`, **yasal iskeletler
>    `noindex`** (yayımlanmamış bir politikanın arama sonucunda yayımlanmış gibi
>    görünmesi kabul edilemez), diğer her ekran değişmeden `noindex`.
> 4. **Yasal metinler YAZILMADI, iskele kuruldu (Q23 açık kalıyor).** Dördünün de
>    rotası, footer bağlantısı ve görünür "bu metin henüz yayımlanmadı" bloğu var;
>    her sayfa **neyi beklediğini madde madde basıyor**. Gerçek şirket künyesi,
>    veri sorumlusu, saklama süresi ve iletişim adresi bu repoda yok ve
>    uydurulmuş bir belge hukuken yanlış **ve bitmiş görünümlü** olurdu.
>    **Tek istisna çerez bilgilendirmesi:** hangi çerezlerin konduğu **kodda**, o
>    yüzden tablo çerez sabitlerinden **türetiliyor** ve
>    `TestCookieNotice_ListsExactlyTheCookiesTheProductSets` yedinci bir çerez
>    eklendiğinde kırmızıya döner.
> 5. **🔴 KIRMIZI ÇİZGİ TARAYICISI DEĞİŞTİ — [ADR 0012](../adr/0012-r1-tarayicisinin-pazarlama-muafiyeti.md).**
>    Kart bir parmak izi terminaliyle karşılaştırma tablosu istiyor, yani sayfa
>    §4.1'i **inkâr etmek için adlandırmak** zorunda. `redline-check.sh`'in R1
>    kuralına **tek ifadelik, yola sınırlı, her koşuda WARN basan** bir muafiyet
>    eklendi; ikinci ifade `activate.templ`'in kalıbı kullanılarak **gereksiz
>    kılındı**. Kabul edilen risk ve koşturulan beş sonda ADR'de.
> 6. **🔴 SAYFAYA YENİ CÜMLE EKLEYEN HERKES İÇİN — `pages.Claim` + `pages.Anchor`.**
>    2. turda denetim, sayfada **mount edilmemiş bir yetenek ilan eden** bir cümle
>    buldu: *"Reports and the monthly headcount split by department"* — iki yarısı
>    da yanlıştı (`reports.templ`'de `department` **0** kez; `ledger.Report`'ta
>    departman boyutu yok; billing yüzeyinin **beş** dosyasında da **0**). Yapısal
>    sebep cümle değil **pin yokluğuydu**: fiyat, bedava ay, çerez listesi, slogan
>    ve karşılaştırma tablosu pinliydi, bu blok değildi.
>    **Kural artık:** "iki şekil" bloğundaki her cümle bir `Anchor` **adlandırmak
>    zorunda**, ve `internal/handler`'ın `anchorDerivations` fonksiyonu her
>    anchor'ı **şemadan / üretilen sorgu tiplerinden / policy baseline'ından /
>    domain kaynağından TÜRETİR** — metin karşılaştırması **yok** (elle tutulan
>    liste bu repoda her seferinde bayatladı). Anchor'ı olmayan cümle **derlenir
>    ama testi kırar**; türetilemeyen cümle **yazılmaz**. M7-02 fiyat/VAT/kayıt
>    metni eklerken aynı disiplini sürdürmeli.
>    > 🔴 **AMA ÇAPA CÜMLEYİ DOĞRU YAPMAZ — bunu okumadan cümle ekleme.** Mekanizma
>    > yalnız **yeteneği KAYBOLAN** iddiayı yakalar; **hiç var olmamış** bir yeteneği
>    > ya da **ilgisiz bir çapaya iliştirilmiş** bir cümleyi yapısal olarak göremez.
>    > Denetim ikisini birden kanıtladı: geçen bir çapaya uydurma bir cümle
>    > (*"Tappa emails every manager a nightly summary…"*) iliştirdi, paket **yeşil**
>    > kaldı. Çapa **sürüklenmeye karşı bir cırcır**, doğruluk kanıtı değil — yeni
>    > cümleyi yine **bir insan üründe doğrulamak zorunda**. B1'de olmayan tam olarak
>    > buydu.

---

## M7-02 — Kayıt sihirbazı ve VAT

- **Bağımlılık:** M7-01 · M1-02 · Q09
- **Commit:** `feat(signup): add three-step registration wizard`

**Adımlar** (handoff §9): firma adı → VAT + işletme tipi çipleri
(Restaurant/Café/Bar/Kiosk/Retail/Hotel/Production/Other) → Single vs Multi
location + dinamik lokasyon listesi → admin hesabı → APPROVED damgalı done ekranı.

**Kabul kriterleri.**
- VAT zorunlu (gerçek işletme filtresi + fatura ön-dolumu).
- VIES davranışı Q09 kararına göre; servis kesintisi kayıt akışını **durdurmuyor**.
- `structure` (`single|multi`) seçimi lokasyon/departman modelini belirliyor.
- Sunucu tarafı doğrulama tam; istemci doğrulaması yalnızca kolaylık.
- Bot koruması (rate limit + basit challenge), CAPTCHA üçüncü parti değil.

---

## M7-03 — Tenant provisioning

- **Bağımlılık:** M7-02 · M1-10
- **Kırmızı çizgi:** §4.5
- **Commit:** `feat(signup): provision tenant, locations and first admin`

**Kabul kriterleri.**
- Tek transaction: tenant + lokasyonlar + (varsa) departmanlar + admin kullanıcı.
- Yeni tenant'ın verisi mevcut tenant'lardan tamamen izole (RLS testi bu akış
  için de koşuyor).
- İlk plaket kaydı için "tag bekleniyor" durumu; UID sonradan bağlanıyor.
- Başarısızlıkta yarım tenant kalmıyor (rollback).

---

## M7-04 — Admin daveti, şifre sıfırlama, e-posta

- **Bağımlılık:** M7-03 · Q02
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(auth): add admin invite and password reset`

**Kabul kriterleri.**
- Magic link ve/veya şifre ile giriş (handoff §9).
- Sıfırlama token'ı tek kullanımlık, süreli, hash'i saklanıyor.
- E-posta sağlayıcısı AB bölgesinde, GDPR işleme sözleşmesi var (Q02).
- Gönderim başarısızlığı kullanıcıya dürüstçe bildiriliyor, sessiz yutulmuyor.
- Token/kod log'a **yazılmıyor**.

---

## M7-05 — Hesap ve marka mesajı ayarları

- **Bağımlılık:** M7-03 · M6-01
- **Commit:** `feat(dashboard): add account settings`

**Kabul kriterleri.**
- Firma bilgileri, fatura verileri, plan görünümü.
- Zaman dilimi ayarı (Q01 kararına göre tenant/lokasyon seviyesinde).
- Marka mesajları görünür (düzenleme editörü M9-04); varsayılanlar KF/KM
  metinleri.
- Her değişiklik `audit_log`'a yazılıyor.
