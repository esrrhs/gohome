package dns

// DefaultDirectTLDs 默认视为境内的顶级域名与国家顶级域
var DefaultDirectTLDs = []string{
	"cn",
	"xn--fiqs8s", // .中国
	"xn--55qx5d", // .公司
	"xn--io0a7i", // .网络
}

// DefaultChinaMainDomains 常见的境内主流主干域名（开箱即用高频命中）
var DefaultChinaMainDomains = []string{
	"baidu.com", "baidupcs.com", "bdimg.com", "bdstatic.com",
	"qq.com", "tencent.com", "weixin.com", "wechat.com", "qpic.cn", "gtimg.com",
	"taobao.com", "alipay.com", "alibaba.com", "aliyun.com", "alicdn.com", "tmall.com",
	"jd.com", "360buy.com", "360buyimg.com", "jcloud.com",
	"bilibili.com", "bilivideo.com", "hdslb.com",
	"bytedance.com", "douyin.com", "douyinvod.com", "tiktokcdn.com", "toutiao.com",
	"163.com", "126.net", "netease.com",
	"sina.com.cn", "weibo.com", "weibocdn.com",
	"zhihu.com", "zhimg.com",
	"meituan.com", "dianping.com",
	"kuaishou.com", "yximgs.com",
	"xiaomi.com", "mi.com", "miui.com",
	"huawei.com", "vmall.com", "hicloud.com",
	"apple.com.cn", "apple.com", // apple 国内 CDN 优化直连可根据需要配置
}

// DefaultDomesticDNS 默认境内直连 DNS 列表 (UDP 及 DoH)
var DefaultDomesticDNS = []string{
	"223.5.5.5:53",
	"119.29.29.29:53",
	"180.184.1.1:53",
}

// DefaultRemoteDoH 默认境外加密安全 DNS (DoH)
var DefaultRemoteDoH = []string{
	"https://1.1.1.1/dns-query",
	"https://8.8.8.8/dns-query",
}

// DefaultReservedCIDRs RFC 标准局域网与回环私有地址
var DefaultReservedCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"100.64.0.0/10", // CGNAT
	"::1/128",
	"fc00::/7",
	"fe80::/10",
}
