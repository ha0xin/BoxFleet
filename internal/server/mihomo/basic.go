package mihomo

const builtInBasicYAML = `
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
unified-delay: true
tcp-concurrent: true

dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://1.1.1.1/dns-query

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - AUTO
      - DIRECT
    include-all-proxies: true
    exclude-type: direct
  - name: AUTO
    type: url-test
    include-all-proxies: true
    exclude-type: direct
    url: https://www.gstatic.com/generate_204
    interval: 300

rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`

func BuiltInBasicRewrite() Rewrite {
	return Rewrite{
		Name:    "BoxFleet Basic",
		Kind:    RewriteYAML,
		Content: builtInBasicYAML,
	}
}
