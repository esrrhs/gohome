package dns

import (
	"strings"
	"sync"
)

// RegionPreset 定义特定国家/地区的网络与直连跳过预设
type RegionPreset struct {
	// ISO 3166-1 alpha-2 两位国家代码 (如 "CN", "IR", "RU", "TR", "MM", "PK", "VN", "EG", "AE", "SA", "IN")
	Code string
	// 国家名称描述
	Name string
	// 默认本国直连顶级域名及国际化国家代码顶级域 (IDN TLD)
	DirectTLDs []string
	// 默认本国直连 DNS 上游服务器列表 (IP:Port)
	DirectUpstreams []string
	// 默认本国核心主干域名（政务、银行、出行、本土电商、流媒体与通讯，防止海外代理触发 403 阻断）
	DirectDomains []string
}

var (
	presetMu sync.RWMutex
	presets  = map[string]RegionPreset{}
	// 保留预设展示顺序
	presetOrder = []string{"CN", "IR", "RU", "TR", "MM", "PK", "VN", "EG", "AE", "SA", "IN", "NONE"}
)

func init() {
	// 初始化内置国家预设表
	registerBuiltinPresets()
}

func registerBuiltinPresets() {
	// 1. 中国 (China)
	RegisterRegionPreset(RegionPreset{
		Code:            "CN",
		Name:            "中国 (China)",
		DirectTLDs:      DefaultDirectTLDs,
		DirectUpstreams: DefaultDomesticDNS,
		DirectDomains:   nil, // 运行时动态使用 DefaultChinaMainDomains，避免 init 顺序依赖
	})

	// 2. 伊朗 (Iran) - 拥有 NIN 国家信息网与严格海外 IP 封锁
	RegisterRegionPreset(RegionPreset{
		Code: "IR",
		Name: "伊朗 (Iran)",
		DirectTLDs: []string{
			"ir",
			"xn--mgba3a4f16a", // .ایران
		},
		DirectUpstreams: []string{
			"178.22.122.100:53", // Shecan 反污染直连 DNS
			"185.51.200.2:53",   // Shecan 备用
			"10.202.10.202:53",  // Radar403
		},
		DirectDomains: []string{
			// 支付网关与国家银行（对海外 IP 极度敏感）
			"shaparak.ir", "bmi.ir", "bankmellat.ir", "mellatbank.com", "tejaratbank.ir",
			"bpi.ir", "samanepay.com", "parsian-bank.ir", "enbank.ir", "sadadpsp.ir",
			// 本地出行与外卖
			"snapp.ir", "snapp.market", "snappfood.ir", "snapptrip.com", "tapsi.ir",
			// 本地电商与分类广告
			"digikala.com", "dkstatics-public.digikala.com", "divar.ir", "torob.com",
			"emalls.ir", "basalam.com", "sheypoor.com",
			// 本地流媒体与视频
			"aparat.com", "telewebion.com", "filimo.com", "namava.ir",
			// 本地即时通信与政务/电信
			"bale.ai", "rubika.ir", "eitaa.com", "gap.im", "soroush-app.ir",
			"gov.ir", "moi.ir", "mci.ir", "irancell.ir", "rightel.ir", "tci.ir",
		},
	})

	// 3. 俄罗斯 (Russia) - TSPU 主权互联网集中过滤与本地化生态
	RegisterRegionPreset(RegionPreset{
		Code: "RU",
		Name: "俄罗斯 (Russia)",
		DirectTLDs: []string{
			"ru", "su",
			"xn--p1ai",  // .рф
			"xn--e1a4c", // .ею
			"xn--c1avg", // .орг
		},
		DirectUpstreams: []string{
			"77.88.8.8:53",      // Yandex.DNS
			"77.88.8.1:53",      // Yandex.DNS Secondary
			"195.208.4.1:53",    // 本地公共 DNS
		},
		DirectDomains: []string{
			// 搜索与门户生态
			"yandex.ru", "yandex.com", "ya.ru", "mail.ru", "dzen.ru", "rambler.ru",
			// 社交与流媒体
			"vk.com", "vk.ru", "ok.ru", "rutube.ru", "kinopoisk.ru", "smotrim.ru",
			// 统一政务与公共服务（外网访问易被阻断）
			"gosuslugi.ru", "nalog.gov.ru", "mos.ru", "kremlin.ru", "cbr.ru", "pfr.gov.ru",
			// 银行与金融系统
			"sberbank.ru", "sber.ru", "tbank.ru", "tinkoff.ru", "vtb.ru", "alfabank.ru",
			"nspk.ru", "mir-plat.ru", "gazprombank.ru", "open.ru",
			// 本土电商与物流出行
			"ozon.ru", "wildberries.ru", "avito.ru", "market.yandex.ru", "cdek.ru", "pochta.ru",
		},
	})

	// 4. 土耳其 (Turkey)
	RegisterRegionPreset(RegionPreset{
		Code: "TR",
		Name: "土耳其 (Turkey)",
		DirectTLDs: []string{
			"tr",
		},
		DirectUpstreams: []string{
			"195.175.39.39:53", // Turk Telekom
			"195.175.39.40:53",
		},
		DirectDomains: []string{
			// 政务与公共服务
			"turkiye.gov.tr", "meb.gov.tr", "saglik.gov.tr", "gib.gov.tr", "egm.gov.tr",
			// 银行金融
			"ziraatbank.com.tr", "isbank.com.tr", "garantibbva.com.tr", "akbank.com",
			"yapikredi.com.tr", "halkbank.com.tr", "vakifbank.com.tr", "papara.com",
			// 本土电商、外卖与出行
			"hepsiburada.com", "trendyol.com", "sahibinden.com", "n11.com", "getir.com", "yemeksepeti.com",
			// 本地电信与主流门户
			"turkcell.com.tr", "vodafone.com.tr", "turktelekom.com.tr", "ensonhaber.com", "hurriyet.com.tr",
		},
	})

	// 5. 缅甸 (Myanmar)
	RegisterRegionPreset(RegionPreset{
		Code: "MM",
		Name: "缅甸 (Myanmar)",
		DirectTLDs: []string{
			"mm",
		},
		DirectUpstreams: []string{
			"203.81.64.18:53", // MPT Myanmar
			"203.81.64.19:53",
		},
		DirectDomains: []string{
			"kbzbank.com", "kbzpay.com", "ayabank.com", "cbbank.com.mm", "wavemoney.com.mm",
			"mabbank.com", "mpt.com.mm", "mytel.com.mm", "atom.com.mm", "shop.com.mm",
			"president-office.gov.mm", "myanmar.gov.mm",
		},
	})

	// 6. 巴基斯坦 (Pakistan)
	RegisterRegionPreset(RegionPreset{
		Code: "PK",
		Name: "巴基斯坦 (Pakistan)",
		DirectTLDs: []string{
			"pk",
			"xn--mgbai9azgqp6j", // .پاکستان
		},
		DirectUpstreams: []string{
			"175.107.248.88:53", // Nayatel
			"202.125.132.2:53",  // PTCL
		},
		DirectDomains: []string{
			"pakistan.gov.pk", "nadra.gov.pk", "fbr.gov.pk", "pass.gov.pk",
			"hbl.com", "ubldigital.com", "mcb.com.pk", "easypaisa.com.pk", "jazzcash.com.pk", "meezanbank.com",
			"daraz.pk", "ptcl.com.pk", "jazz.com.pk", "telenor.com.pk", "zong.com.pk",
		},
	})

	// 7. 越南 (Vietnam)
	RegisterRegionPreset(RegionPreset{
		Code: "VN",
		Name: "越南 (Vietnam)",
		DirectTLDs: []string{
			"vn",
		},
		DirectUpstreams: []string{
			"203.162.0.181:53", // VNPT
			"203.162.0.11:53",
			"203.113.131.1:53", // Viettel
		},
		DirectDomains: []string{
			"zalo.me", "vng.com.vn", "fpt.com.vn", "viettel.vn", "vnpt.com.vn",
			"vietcombank.com.vn", "techcombank.com.vn", "momo.vn", "vnpay.vn", "bidv.com.vn", "vietinbank.vn",
			"shopee.vn", "tiki.vn", "lazada.vn", "vnexpress.net", "dantri.com.vn", "chotot.com",
		},
	})

	// 8. 埃及 (Egypt)
	RegisterRegionPreset(RegionPreset{
		Code: "EG",
		Name: "埃及 (Egypt)",
		DirectTLDs: []string{
			"eg",
			"xn--wgbh1c", // .مصر
		},
		DirectUpstreams: []string{
			"163.121.128.134:53", // Telecom Egypt
			"163.121.128.135:53",
		},
		DirectDomains: []string{
			"digital.gov.eg", "egypt.gov.eg", "mohp.gov.eg",
			"fawry.com", "cibeg.com", "nbe.com.eg", "banquemisr.com", "qnb.com.eg",
			"te.eg", "vodafone.com.eg", "orange.eg", "etisalat.eg", "jumia.com.eg", "amazon.eg",
		},
	})

	// 9. 阿联酋 (UAE)
	RegisterRegionPreset(RegionPreset{
		Code: "AE",
		Name: "阿联酋 (United Arab Emirates)",
		DirectTLDs: []string{
			"ae",
			"xn--mgbaam7a8h", // .امارات
		},
		DirectUpstreams: []string{
			"213.42.20.20:53",   // Etisalat
			"195.229.241.50:53", // Etisalat Secondary
		},
		DirectDomains: []string{
			"u.ae", "dubai.ae", "icp.gov.ae", "mohre.gov.ae", "rta.ae",
			"enbd.com", "adcb.com", "fab.com", "mashreqbank.com", "alansari.com",
			"careem.com", "noon.com", "talabat.com", "etisalat.ae", "du.ae",
		},
	})

	// 10. 沙特阿拉伯 (Saudi Arabia)
	RegisterRegionPreset(RegionPreset{
		Code: "SA",
		Name: "沙特阿拉伯 (Saudi Arabia)",
		DirectTLDs: []string{
			"sa",
			"xn--mgberp4a5d4ar", // .السعودية
		},
		DirectUpstreams: []string{
			"212.118.133.100:53", // STC
			"212.118.133.101:53",
		},
		DirectDomains: []string{
			"absher.sa", "my.gov.sa", "qiwa.sa", "zatca.gov.sa", "sehhaty.sa",
			"alrajhibank.com.sa", "alahli.com", "riyadbank.com", "alinma.com", "stcpay.com.sa", "urpay.com.sa",
			"stc.com.sa", "mobily.com.sa", "noon.com", "hungerstation.com", "jahez.net", "haraj.com.sa",
		},
	})

	// 11. 印度 (India)
	RegisterRegionPreset(RegionPreset{
		Code: "IN",
		Name: "印度 (India)",
		DirectTLDs: []string{
			"in",
			"xn--h2brj9c", // .भारत
			"xn--2scrj9c", // .ভারত
			"xn--45brj9c", // .ভাৰত
		},
		DirectUpstreams: []string{
			"180.179.206.182:53", // NIC (National Informatics Centre)
			"122.160.237.1:53",   // Airtel
		},
		DirectDomains: []string{
			"uidai.gov.in", "incometax.gov.in", "irctc.co.in", "passportindia.gov.in", "digilocker.gov.in", "india.gov.in",
			"sbi.co.in", "hdfcbank.com", "icicibank.com", "axisbank.com", "paytm.com", "phonepe.com", "npci.org.in",
			"jio.com", "airtel.in", "flipkart.com", "hotstar.com",
		},
	})

	// 12. 纯净模式 (NONE / GLOBAL)
	RegisterRegionPreset(RegionPreset{
		Code:            "NONE",
		Name:            "纯净无预设模式 (None / Global)",
		DirectTLDs:      nil,
		DirectUpstreams: nil,
		DirectDomains:   nil,
	})
}

// RegisterRegionPreset 注册或替换一个国家区域预设
func RegisterRegionPreset(preset RegionPreset) {
	presetMu.Lock()
	defer presetMu.Unlock()
	upper := strings.ToUpper(strings.TrimSpace(preset.Code))
	if upper == "" {
		return
	}
	preset.Code = upper
	presets[upper] = preset
}

// GetRegionPreset 获取指定国家代码的预设信息。支持不区分大小写
func GetRegionPreset(code string) (RegionPreset, bool) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if upper == "" {
		return RegionPreset{}, false
	}
	if upper == "GLOBAL" {
		upper = "NONE"
	}

	presetMu.RLock()
	defer presetMu.RUnlock()

	p, ok := presets[upper]
	if !ok {
		return RegionPreset{}, false
	}

	// 深拷贝避免外部修改内部 slice
	cp := RegionPreset{
		Code:            p.Code,
		Name:            p.Name,
		DirectTLDs:      append([]string{}, p.DirectTLDs...),
		DirectUpstreams: append([]string{}, p.DirectUpstreams...),
	}

	if upper == "CN" {
		// CN 特殊处理：使用 DefaultChinaMainDomains（已合并 seed + embed china-list）
		cp.DirectDomains = append([]string{}, DefaultChinaMainDomains...)
	} else {
		cp.DirectDomains = append([]string{}, p.DirectDomains...)
	}

	return cp, true
}

// ListRegionPresets 列出所有支持的国家区域预设
func ListRegionPresets() []RegionPreset {
	presetMu.RLock()
	defer presetMu.RUnlock()

	var list []RegionPreset
	seen := make(map[string]bool)

	// 先按标准推荐顺序添加
	for _, code := range presetOrder {
		if _, ok := presets[code]; ok {
			cp, _ := GetRegionPreset(code)
			list = append(list, cp)
			seen[code] = true
		}
	}

	// 补充用户动态注册的额外预设
	for code := range presets {
		if !seen[code] {
			cp, _ := GetRegionPreset(code)
			list = append(list, cp)
		}
	}

	return list
}
