# Tappa — Ürün Handoff Dokümanı

> Bu doküman Tappa'nın **ürün gerçeğidir**: ne inşa ettiğimiz, kime, neden.
> Ürün kararı değiştiğinde burası güncellenir.
>
> *Nasıl* çalıştığımız (stack, kod kuralları, kırmızı çizgiler) repo kökündeki
> [CLAUDE.md](../CLAUDE.md) dosyasındadır. Teknik kararların gerekçeleri
> [docs/adr/](adr/) altındadır.
>
> ⚠️ Bölüm 9 ("Mevcut Kod Envanteri") ve bölüm 13 tarihsel kayıttır: o dosyalar
> bu repoya taşınmadı, sıfırdan Go + templ ile yazılıyor. Bkz. [ADR 0001](adr/0001-stack-secimi.md).

---

## 1. Ürün Özeti

**Tappa** — "Punchless time & attendance." NFC tabanlı, tamamen **cihazsız**
mesai takip SaaS'ı.

Ana fikir (kritik — modelin tersine çevrilmiş hali): NFC etiketi çalışanın
cebinde DEĞİL, **duvarda**. Her lokasyonun girişine markalı, A5 boyutlu,
kameranın gördüğü bir noktaya monte edilen pasif bir plaket yapıştırılır
(içinde NTAG 424 DNA çipi). Çalışan **kendi telefonunu** plakete değdirir →
tarayıcı otomatik açılır (uygulama kurulumu YOK) → telefondaki kalıcı
oturumdan tanınır → "Hello Maria" + tek buton → check-in/out biter.

- **Slogan:** *No app. No device. No fingerprints. Just tap.*
- **Alt slogan / kampanya:** *Go punchless.*
- **Plaket kimliği "yeri" söyler, telefon oturumu "kişiyi" söyler,
  IP + GPS "gerçekten orada" olduğunu kanıtlar, SUN imzası "şu anda fiziksel
  dokunuş" olduğunu kanıtlar.**

## 2. Problem (neden var?)

Mevcut durum: müşteriler ZKTeco **BioTime Cloud** + parmak izi cihazı
kullanıyor. Acı noktaları:
- Her yeni çalışan için cihaz başında **enrollment** süreci
- Cihaz arızası = tüm lokasyon kör
- Islak/yağlı/unlu eller sensörde çalışmıyor (mutfak gerçeği)
- Biyometrik veri saklamanın GDPR yükü
- Donanım + bakım maliyeti

Tappa'nın cevabı: sahada bakım gerektiren **hiçbir aktif bileşen yok**.
Plaket pasiftir (pil yok, yazılım yok, internet yok); yırtılırsa yedeği
yapıştırılıp panelden "Replace tag" denir (30 saniye). Onboarding: profil aç
→ davet gönder → çalışan ilk tap'te aktive olur. Offboarding: tek tık →
oturum o saniye ölür.

## 3. Pazar & Rekabet

Pazar ~4,3 Mia USD (2026), ~%8 CAGR. Küme analizi:
- **Enterprise:** ADP, UKG, Paycor, Rippling → hedef pazarımız değil.
- **KOBİ/saha:** Connecteam, Deputy, Jibble, Clockify, Homebase, Hubstaff.
- **En yakın rakip: Connecteam** — telefonla NFC etikete tap özelliği VAR ama:
  (a) uygulama kurulumu + uygulama içinden NFC butonu şart,
  (b) etiket düz sticker → SUN/replay koruması yok, GPS damgasına güveniyor,
  (c) koca suite'in parçası ($29/ay'dan başlayan paket).
- **Jibble:** ters model — kart kiosk'a okutulur (cihaz gerekir).

**Tappa'nın savunulabilir farkları:** (1) app-less akış, (2) NTAG 424 DNA +
SUN kriptografik dokunuş kanıtı, (3) tek işi kusursuz yapan sade ürün,
(4) çalışan başına ucuz fiyat, (5) hospitality odak.

Konumlanma: *"Biz giriş-çıkış gerçeğinin tek doğru kaynağıyız; bordroyu
istediğin sisteme CSV/API ile ver."* Bordro/izin/vardiya planlama MVP'de YOK.

## 4. Marka

- **İsim:** Tappa — "tap" jesti + Malta'ca/İtalyanca *tappa* = rotadaki
  durak/etap (çok lokasyonlu zincir metaforu). ⚠️ Yapılacak: tappa.mt /
  tappa.io domain + EUIPO marka taraması.
- **Palet:** ink `#152219`, porcelain `#EDF0EA`, tappa green `#1F5C41`,
  green-lite `#E1EDE6`, saffron `#D98E2B`, saffron-lite `#F7EBD6`,
  tomato `#BE3D2A`, paper `#FFFDF4`, line `#C9D2C8`.
- **Tipografi:** Space Grotesk (display) + IBM Plex Mono (veri/adisyon).
- **İmza motif:** "mutfak adisyonu" (kitchen docket) — işlem kayıtları
  perforeli fiş kartları olarak gösterilir; APPROVED/FLAGGED/REJECTED
  damgaları hafif eğik "kaşe" stilinde.
- **Marka mesajları** (onaylı işlem sonrası ekranda, tenant'a özel,
  panelden düzenlenebilir olacak):
  - KF check-in: "Have a great shift — keep those kebabs rolling! 🌯"
  - KF check-out: "Great work today. See you next shift! 👋"
  - KM check-in: "Have a productive shift — stay safe on the floor! 🏭"
  - KM check-out: "Shift complete. Thank you for your work today! 👋"

## 5. Hazır Müşteriler (design partner, 100+ kullanıcı)

### Kebab Factory Ltd. — multi-location (9 lokasyon)
| Lokasyon | Vardiya | Not |
|---|---|---|
| KF Hamrun, KF Mellieha, KF Msida, KF Valletta, KF San Gwann, KF St Julians | 10:00–22:00 | restoranlar |
| KF Paceville | 11:00–23:00 | |
| Rusty Bar | **18:00–02:00** | gece vardiyası, çıkış ertesi güne sarkar |
| KF Headquarter | 09:00–17:00 | ofis |

Her lokasyonun **kendi statik dış IP'si** var (ISS'den statik IP alınacak).
KF St Julians referans senaryo: 15 kişilik kadro, günde ~10 kişi, plaket
kamera görüş alanında.

### Kebab Manufacturing Co. Ltd. — single-location, 5 departman
Tek tesis = **tek dış IP**. Departman, çalışanın profilinden çözülür
(IP sadece "tesiste" kanıtı). Vardiyalar **departman bazlı**:
KM General 09:00–17:00 · KM Meat Production 05:00–13:00 ·
KM Warehouse & Supply 08:00–16:00 · KM Central Kitchen 06:00–14:00 ·
KM Pastry & Bakery 04:00–12:00. Geç kalma hesabı kişinin KENDİ vardiyasına
göre yapılır.

Hiyerarşi (şemanın omurgası):
`Tenant → Location (statik IP'ler + GPS koordinatı) → Department (ops.) → Employee`

## 6. Güvenlik Mimarisi — her tap'te 4 kanıt

1. **SUN / proof of moment:** NTAG 424 DNA her okutmada URL'ye artan sayaç
   (`ctr`) + AES-128 CMAC imza gömer:
   `https://time.tappa.mt/t?tag=91AC7E5500000A&ctr=000641&cmac=8F2A...`
   Sunucu: CMAC'i etikete ait gizli anahtarla yeniden hesaplar + `ctr` >
   kayıtlı son ctr kontrolü. Kopyalanmış/replay link → REJECT. ⚠️ ctr
   güncellemesi ATOMİK olmalı (race → replay açığı).
2. **Oturum / proof of person:** Kimlik, `httpOnly + Secure` çerezdeki oturum
   token'ından. Aktivasyon: davet linki → bir kerelik kod → sunucu token
   üretir, **hash'ini** saklar (`token_hash → employee_id, revoked`).
   Uzun ömür (1 yıl, kullanımda yenilenir). İptal anlık. Cihaz limiti
   (1-2) opsiyonel. Telefonun kendi ekran kilidi = bedava biyometrik katman
   (biz biyometri SAKLAMAYIZ).
3. **Statik IP / proof of place:** Kaynak IP lokasyonun IP listesinde →
   "binada" kanıtı + lokasyon otomatik çözülür. Not: lokasyon interneti
   kesilse bile telefonlar mobil veriyle çalışmaya devam eder (okuyuculu
   sistemlere üstünlük).
4. **GPS / yedek kanıt:** IP yoksa GPS <150 m doğrular. GPS yalnızca tap
   ANINDA okunur — sürekli takip YOK (GDPR + satış argümanı).

**Güven puanı:** `20 taban + 50 (IP) + 30 (GPS)`.
**Karar sırası:** kayıp/emekli etiket → REJECT · SUN geçersiz → REJECT ·
hesap deaktive → REJECT (denemesi loglanır + güvenlik uyarısı) · aynı kişi
60 sn içinde → IGNORED (debounce KİŞİ bazlı, etiket bazlı değil — ardışık
farklı kişiler sorunsuz) · IP veya GPS var → APPROVED (IP yoksa nota
"verified via GPS") · ikisi de yok → kayıt alınır, **FLAGGED**, müdür onay
kuyruğuna düşer. Asla sessizce onaylanmaz, asla kayıt kaybedilmez.

**Yön tayini:** kişinin SON AÇIK GİRİŞİNE göre in/out toggle — takvim gününe
göre DEĞİL (Rusty Bar gece vardiyası 02:00 çıkışı doğru eşleşir).

**Kenar durumlar:**
- **QR fallback:** plakette NFC + QR birlikte basılır. QR'da SUN yok →
  sunucu bu kanalda IP/GPS'i zorunlu tutar. `channel: nfc | qr | manual`.
- **Telefonsuz çalışan:** vardiya amiri panelden manuel kayıt,
  `entered_by` etiketiyle; raporlarda ayrı görünür.
- **Practice tap (onboarding eğitimi):** Aktivasyondan hemen sonra 3 slaytlık
  mini tur ("Tap the plaque" → "One button" → "First tap is practice").
  İlk check-in **TRAINING** damgası alır, `practice: true`, saatlere ASLA
  sayılmaz. Eğitim kayıt anında tamamlanır — operasyonel onboarding sıfır.
- **Onay ekranı:** buton YOK, "All done — you can close this page." Bir
  sonraki tap zaten yeni fiziksel dokunuş gerektirir. Başarısız işlemde
  "Try again" var.
- **Plaket değişimi:** "Replace tag" → yeni UID, eski UID `retired`, retired
  etikete tap → REJECT. Geçmiş etiketler audit için saklanır.
- **Immutability:** transactions'a kimlik TAP ANINDA çözülüp yazılır;
  geçmiş kayıtlar sonradan asla değişmez.

## 7. Veri Modeli (PostgreSQL — her tabloda tenant_id + Row-Level Security)

```sql
tenants(id, name, vat_number, business_type, structure, plan, created_at)
locations(id, tenant_id, name, static_ips text[], gps_lat, gps_lng,
          shift_start, shift_end, overnight bool)
departments(id, tenant_id, location_id, name, shift_start, shift_end)
employees(id, tenant_id, location_id, department_id, full_name, role,
          email, status)            -- status: invited|active|deactivated
sessions(id, employee_id, token_hash, device_info, created_at,
         last_used_at, revoked bool)
tags(uid PK, tenant_id, location_id, aes_key_ref, last_ctr, status,
     retired_at)                    -- status: active|retired|lost
transactions(id, tenant_id, employee_id, location_id, department_id,
             tag_uid, ctr, type,    -- type: in|out|null
             occurred_at, source_ip, ip_match bool, gps_lat, gps_lng,
             gps_match bool, sun_valid bool, trust smallint,
             verdict,               -- ok|flag|reject|ignored
             note, channel,         -- nfc|qr|manual
             entered_by, practice bool)
audit_log(id, tenant_id, actor_id, action, target, at)  -- düzeltme/silme izi
```

## 8. API Sözleşmesi (taslak)

```
POST /api/signup        {company, vat, business_type, structure, locations[],
                         admin{name,email,pass}}  → VAT: VIES doğrulaması
POST /api/auth/login    → dashboard oturumu
POST /api/activate      {invite_code} → httpOnly çerez set eder
GET  /t?tag&ctr&cmac    → tap sayfası (SSR/SPA); sunucu SUN ön-doğrulama
POST /api/checkin       {tag, ctr, cmac, gps?, practice?} → karar + kayıt
GET  /api/records?date&location&employee → panel + dış sistem entegrasyonu
GET  /api/export.csv    → günlük/haftalık rapor
CRUD /api/locations /api/departments /api/employees /api/tags
POST /api/employees/:id/deactivate | /reinvite
POST /api/tags/:uid/replace
POST /api/records/manual {employee_id, type, time} → entered_by otomatik
```

Çevrimdışı kuyruk: PWA istemcisi başarısız POST'u lokalde kuyruğa alır,
online olunca yeniden dener; sunucu istemci `time` değerini "queued"
etiketiyle kabul eder (imzalı ctr geç senkronda da geçerli).

## 9. Mevcut Kod Envanteri

```
web/tappa-landing.html   Pazarlama sitesi. Hero: canlı plaket + basılan
                         adisyon animasyonu. Bölümler: 3 adım, fingerprint
                         karşılaştırma tablosu, 4 kanıt, chain/facility,
                         fiyat (€1.50 + 3 ay ücretsiz), FAQ, CTA.
                         Nav → portal (Sign in / Start free).
web/tappa-portal.html    Login (şifre + magic link) ve 3 adımlı kayıt
                         sihirbazı: firma adı, VAT (format kontrolü; gerçekte
                         VIES), işletme tipi çipleri (Restaurant/Café/Bar/
                         Kiosk/Retail/Hotel/Production/Other), Single vs
                         Multi location kartları, dinamik lokasyon listesi,
                         admin hesabı, APPROVED'lu done ekranı. Backend'siz.
pwa/                     Çalışan tap sayfası PWA starter'ı: manifest, sw.js
                         (shell cache, offline fallback, push iskeleti),
                         index.html (aktivasyon → mini tur → practice tap →
                         check-in/out, çevrimdışı kuyruk, install prompt,
                         bildirim testi). Kodda "DEMO SHORTCUTS" ve "REAL:"
                         yorumları gerçek entegrasyon noktalarını işaretler:
                         oturum localStorage→httpOnly çerez, kayıt→POST
                         /api/checkin, kuyruk flush→POST retry.
```

Ayrıca sohbette geliştirilen **React simülasyonu** (nfc-mesai-simulasyonu.jsx)
demo aracı olarak mevcut: iki tenant'lı telefon + admin panel simülasyonu,
"Simulate a full day @ KF St Julians" jeneratörü (10 kişi, 21 işlem,
geç kalma / çift tap / mobil veri / FLAGGED senaryolarıyla).

## 10. Yol Haritası (öncelik sırasıyla)

1. **Admin Dashboard** — portal "Go to my dashboard"ın hedefi. Sekmeler:
   Transactions (adisyon kartları + FLAGGED onay kuyruğu), Employees
   (invite/deactivate/re-invite, oturum durumu), Locations & Wall Tags
   (IP listesi, tag UID, Replace tag, tag geçmişi), Reports (günlük saatler,
   geç kalmalar, CSV export). Marka diline sadık (docket motifi).
2. **Backend monolith** — Node (Fastify/Nest) veya Python (FastAPI) +
   PostgreSQL + RLS. Mikroservis YOK. Bölüm 7-8'deki şema/uçlar.
3. **SUN doğrulama servisi** — NTAG 424 DNA SDM doğrulama: AES-CMAC + ctr
   monotonluk (atomik). Test için 10'luk NTAG 424 DNA etiket (~30 €) +
   NXP TagWriter ile encode; ölçekte encode'lu tedarikçi.
4. **PWA'yı backend'e bağla** — DEMO SHORTCUTS blokları değişir; web push
   (VAPID) abone akışı.
5. **Deploy** — tek VPS (Hetzner/DO, AB bölgesi) + managed Postgres.
6. **Pilot** — KF St Julians tek şube, 1 hafta BioTime ile PARALEL çalıştır,
   kayıt karşılaştırma tablosu çıkar (satış slaytı olur) → iki firmaya tam
   yayılım → BioTime kapanır.
7. Sonrası: yönetici PWA bildirimleri, CSV çalışan içe aktarma (BioTime
   export'undan), tenant marka mesajı editörü, çoklu plaket/lokasyon.

## 11. Fiyatlandırma & GTM

- **€1.50 / çalışan / ay** — plaketler dahil ve ücretsiz değişim.
- **Founding offer:** ilk 3 ay ücretsiz + ömür boyu fiyat sabit; karşılığında
  referans + vaka çalışması.
- Kayıtta **VAT zorunlu** (gerçek işletme filtresi + fatura ön-dolumu).
- Beklenen başlangıç MRR: iki firma ~€200-300/ay; altyapı ~€30-50/ay.

## 12. Kırmızı Çizgiler

- Biyometrik veri ASLA toplanmaz/saklanmaz.
- GPS yalnızca tap anında; sürekli konum takibi YOK.
- transactions immutable; her düzeltme audit_log'a yazılır (mesai kaydı
  hukuki delil niteliğinde olabilir).
- ctr kontrolü atomik (replay koruması).
- Tenant izolasyonu her katmanda (RLS + sorgu filtreleri); kart/etiket
  UID'leri bile tenant'a bağlı düşünülür.
- Kriz senaryosunda kayıt asla reddedilip kaybedilmez → FLAGGED olarak alınır.

## 13. Claude Code için başlangıç komutları

```
1) "CLAUDE.md'yi oku. Yol haritası 1. madde: admin dashboard'u
   web/tappa-dashboard.html olarak, marka diline (bölüm 4) sadık kalarak
   tasarla; sahte veri olarak bölüm 5'teki müşterileri kullan."
2) "Yol haritası 2: FastAPI + PostgreSQL iskeletini kur; bölüm 7 şemasını
   migration olarak yaz, bölüm 8 uçlarının boş handler'larını oluştur."
3) "Bölüm 6'daki SUN doğrulamasını ayrı modül olarak yaz ve birim
   testleriyle kanıtla (geçerli imza, sayaç tekrarı, yanlış anahtar)."
```
