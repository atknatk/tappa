# ADR 0012 — R1 biyometri tarayıcısına pazarlama yüzeyi için tek ifadelik, yola sınırlı, görünür bir muafiyet

- **Durum:** kabul edildi
- **Tarih:** 2026-08-13
- **Karar veren:** M7-01 (landing sayfası) yapıcısı; `tappa-security-auditor`
  5. turda ONAY verdi ve muafiyetin **ilk hâlini kırdı** — bu ADR o kırılmadan
  sonraki hâli kaydeder.
- **Etkilenen:** [`scripts/redline-check.sh`](../../scripts/redline-check.sh) (R1)
  · [`web/templates/pages/landing.templ`](../../web/templates/pages/landing.templ)
  · [`web/templates/pages/landingview.go`](../../web/templates/pages/landingview.go)
  · [`internal/handler/marketing.go`](../../internal/handler/marketing.go)
  · [`internal/handler/marketing_test.go`](../../internal/handler/marketing_test.go)
- **Migration YOK.** Şema değişmedi. Bu bir **güvenlik sınırı** kararıdır:
  CLAUDE.md §4.1'i zorlayan mekanik ağın kapsamı daraltıldı.
- **§4.1 DEĞİŞMEDİ.** Ürün hâlâ hiçbir biyometrik veri toplamıyor/saklamıyor.
  Değişen yalnızca **tarayıcının** hangi satırları ihlal saydığı.

---

## Karar

`scripts/redline-check.sh`'in R1 kuralı (*"Biyometrik veri izi"*), **tek bir
ifadeyi** — `fingerprint terminal` — **yalnızca pazarlama yüzeyinin dosyalarında**
muaf tutar, **ve kullanılan her muafiyeti her koşuda `WARN` olarak basar.**

Muafiyet **üç koşul birden** sağlanınca geçerlidir:

1. yol şu kümede: `web/templates/pages/landing*.{templ,go}` ·
   `internal/handler/marketing*.go`
2. satır `fingerprint terminal` içeriyor,
3. **ve o ifade çıkarıldığında geriye hiçbir tetikleyici kalmıyor.**

Üçüncü koşul muafiyeti *"bu satırı görmezden gel"*ten *"bu **ifadenin kendisi**
bir ihlal değil"*e çevirir.

## Neden gerekti

M7-01 kartı (handoff §9) landing sayfasında bir **parmak izi terminaliyle
karşılaştırma tablosu** istiyor. Yani üründe ilk kez, §4.1'i **ihlal etmeyen**
değil §4.1'i **ilan eden** metin bu terimi kullanmak zorunda: bir tabloyu
*"karşılaştırdığı şeyin adını anmadan"* yazmak mümkün değil.

Ölçüldü: M7-01 öncesi ağaç bu taramada **temizdi**; tüm yeni eşleşmeler yalnızca
pazarlama metninden ve onun testinden geliyordu.

## Elenen alternatifler

| Alternatif | Neden elendi |
|---|---|
| **Metni yeniden yaz, muafiyet ekleme** | **KISMEN İŞE YARADI ve o kısım uygulandı** — bkz. aşağısı. Ama tablonun başlığı, `caption`'ı ve sütun başlığı için işe yaramıyor: ifade tablonun **öznesi**. |
| **Pazarlama dosyalarını R1'den tümüyle muaf tut** | Gerçek bir ihlal (`fingerprintTemplate []byte`) o dosyalarda gizlenebilirdi. Ölçüldü: P3 sondası artık **yakalanıyor**. |
| **R1'i tümden kaldır / FAIL'i WARN'a indir** | Kırmızı çizgi taramasının tek FAIL'i budur; §4.1 ürünün varlık sebebi. |
| **`no biometric` ifadesini de ekle** | **GEREKSİZDİ — reponun kendi karşı örneği vardı.** Bkz. aşağısı. |

### Genişletme yarıya indi: reponun kendi kalıbı

İlk hâl **iki** ifade ekliyordu. `no biometric` **kaldırıldı**, çünkü
`web/templates/pages/activate.templ:76` M5-02'den beri şöyle yazıyor:

> *"Tappa collects **no biometric** data of any kind: **no fingerprints**, no face, no voice."*

Bu satır **eski** süzgeçten geçiyor — aynı satırdaki `no fingerprints`, mevcut
`no.?fingerprint` girdisine uyuyor. Pazarlama metni (footer cümlesi ve
karşılaştırma tablosunun Tappa hücresi) **o kalıba getirildi**, ve ifade
allowlist'ten çıkarıldı.

İki kazanç birden: genişletme **yarıya** indi, **ve** kamuya açık sayfa ile
çalışanın okuduğu ekran §4.1 sözünü artık **aynı kelimelerle** veriyor.

## Kabul edilen risk

🔴 **`grep -v` SATIR YERELDİR.** Muaf yollardaki bir dosyada, `fingerprint
terminal` taşıyan **ve başka hiçbir tetikleyici içermeyen** bir satırdaki gerçek
bir ihlal muaf kalır. Denetçi bunu **ölçtü**.

Üçüncü koşul bunu daralttı ama **kapatmadı**: aynı satırda `webauthn`,
`biometric`, `touch id`, `face id` ya da başka bir `fingerprint` geçen sonda artık
**FAIL** üretiyor (P1, P4, P5). Kalan açık, ifadenin **tek başına** taşıyıcı
olarak kullanıldığı dar durumdur.

⚠️ **VE BU YENİ BİR AÇIK SINIFI DEĞİL.** Mevcut `not stored` ve `asla` girdileri
aynı zayıflığı taşıyor — **`asla` Türkçe yorumlarda çok yaygın olduğu için bu
ADR'nin eklediği ifadeden DAHA GENİŞ**. Betiğin kendi başlığındaki cümlenin sebebi
budur: *"Bu MEKANİK bir ağdır, kanıt değildir."* Derin denetim:
agent `tappa-security-auditor`.

## Görünürlük — R5'in ilkesi R1'e taşındı

İlk hâl **hiçbir koşuda raporlanmıyordu** ve denetçi tam olarak bunu kırdı:

```
internal/handler/marketing.go'ya:
  // PROBE: fingerprint terminal -- keep the webauthn attestation
-> redline exit 0, R1 hiç raporlanmadan
```

R5'in muafiyeti (`-- redline: no-tenant-scope(x) — gerekçe`) **katı sözdizimi**
ister, **yalnız ilgili migration'a** yazılır ve **her koşuda WARN basar** —
*"muafiyet görünmez olamaz."* R1 artık aynı ilkeyi izliyor: kullanılan her
muafiyet `[R1 · WARN]` bloğunda **adıyla ve satırıyla** listelenir.

**Bu blok aynı zamanda CANLI LİSTEDİR.** Betikte muaf satır sayısı **yazmıyor** —
bu dosyada elle yazılan bir sayı bir kez zaten bayatladı (*"kalan dördü sayfanın
görünen metnidir"* derken gerçekte dokuz satırdı ve üçü görünen metin değildi).
Üreten komut `./scripts/redline-check.sh`'in kendisidir.

## Sondalar — hepsi koşturuldu

| # | Sonda | Sonuç |
|---|---|---|
| P1 | denetçinin sondası (ifade + `webauthn`), muaf yolda | **R1 FAIL** |
| P2 | aynı ifade, muaf **olmayan** yolda (`internal/domain/tap`) | **R1 FAIL** |
| P3 | `var fingerprintTemplate []byte`, muaf yolda, ifade yok | **R1 FAIL** |
| P4 | ifade + `biometric template`, tek satır, muaf yolda | **R1 FAIL** |
| P5 | ifade + `TouchID`, tek satır, muaf yolda | **R1 FAIL** |
| — | temiz ağaç | exit 0, 6 satır `[R1 · WARN]` |
