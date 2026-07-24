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
