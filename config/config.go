//go:generate go run github.com/abice/go-enum -f=$GOFILE --marshal --names
package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf"
	"github.com/miekg/dns"

	"github.com/hako/durafmt"

	"github.com/0xERR0R/blocky/log"
	"github.com/creasty/defaults"
)

const (
	udpPort   = 53
	tlsPort   = 853
	httpsPort = 443
)

// NetProtocol resolver protocol ENUM(
// tcp+udp // TCP and UDP protocols
// tcp-tls // TCP-TLS protocol
// https // HTTPS protocol
// )
type NetProtocol uint16

// IPVersion represents IP protocol version(s). ENUM(
// dual // IPv4 and IPv6
// v4   // IPv4 only
// v6   // IPv6 only
// )
type IPVersion uint8

func (ipv IPVersion) Net() string {
	switch ipv {
	case IPVersionDual:
		return "ip"
	case IPVersionV4:
		return "ip4"
	case IPVersionV6:
		return "ip6"
	}

	panic(fmt.Errorf("bad value: %s", ipv))
}

func (ipv IPVersion) QTypes() []dns.Type {
	switch ipv {
	case IPVersionDual:
		return []dns.Type{dns.Type(dns.TypeA), dns.Type(dns.TypeAAAA)}
	case IPVersionV4:
		return []dns.Type{dns.Type(dns.TypeA)}
	case IPVersionV6:
		return []dns.Type{dns.Type(dns.TypeAAAA)}
	}

	panic(fmt.Errorf("bad value: %s", ipv))
}

// QueryLogType type of the query log ENUM(
// console // use logger as fallback
// none // no logging
// mysql // MySQL or MariaDB database
// postgresql // PostgreSQL database
// csv // CSV file per day
// csv-client // CSV file per day and client
// )
type QueryLogType int16

// StartStrategyType upstart strategy ENUM(
// blocking // synchronously download blocking lists on startup
// failOnError // synchronously download blocking lists on startup and shutdown on error
// fast // asyncronously download blocking lists on startup
// )
type StartStrategyType uint16

// QueryLogField data field to be logged
// ENUM(clientIP,clientName,responseReason,responseAnswer,question,duration)
type QueryLogField string

type QType dns.Type

func (c QType) String() string {
	return dns.Type(c).String()
}

type QTypeSet map[QType]struct{}

func NewQTypeSet(qTypes ...dns.Type) QTypeSet {
	s := make(QTypeSet, len(qTypes))

	for _, qType := range qTypes {
		s.Insert(qType)
	}

	return s
}

func (s QTypeSet) Contains(qType dns.Type) bool {
	_, found := s[QType(qType)]

	return found
}

func (s *QTypeSet) Insert(qType dns.Type) {
	if *s == nil {
		*s = make(QTypeSet, 1)
	}

	(*s)[QType(qType)] = struct{}{}
}

type Duration time.Duration

func (c *Duration) String() string {
	return durafmt.Parse(time.Duration(*c)).String()
}

//nolint:gochecknoglobals
var netDefaultPort = map[NetProtocol]uint16{
	NetProtocolTcpUdp: udpPort,
	NetProtocolTcpTls: tlsPort,
	NetProtocolHttps:  httpsPort,
}

// Upstream is the definition of external DNS server
type Upstream struct {
	Net        NetProtocol
	Host       string
	Port       uint16
	Path       string
	CommonName string // Common Name to use for certificate verification; optional. "" uses .Host
}

// IsDefault returns true if u is the default value
func (u *Upstream) IsDefault() bool {
	return *u == Upstream{}
}

// String returns the string representation of u
func (u *Upstream) String() string {
	if u.IsDefault() {
		return "no upstream"
	}

	var sb strings.Builder

	sb.WriteString(u.Net.String())
	sb.WriteRune(':')

	if u.Net == NetProtocolHttps {
		sb.WriteString("//")
	}

	isIPv6 := strings.ContainsRune(u.Host, ':')
	if isIPv6 {
		sb.WriteRune('[')
		sb.WriteString(u.Host)
		sb.WriteRune(']')
	} else {
		sb.WriteString(u.Host)
	}

	if u.Port != netDefaultPort[u.Net] {
		sb.WriteRune(':')
		sb.WriteString(fmt.Sprint(u.Port))
	}

	if u.Path != "" {
		sb.WriteString(u.Path)
	}

	return sb.String()
}

// ListenConfig is a list of address(es) to listen on
type ListenConfig []string

// UnmarshalYAML creates BootstrapConfig from YAML
func (b *BootstrapConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if err := unmarshal(&b.Upstream); err == nil {
		return nil
	}

	// bootstrapConfig is used to avoid infinite recursion:
	// if we used BootstrapConfig, unmarshal would just call us again.
	var c bootstrapConfig
	if err := unmarshal(&c); err != nil {
		return err
	}

	*b = BootstrapConfig(c)

	return nil
}

// UnmarshalYAML creates ConditionalUpstreamMapping from YAML
func (c *ConditionalUpstreamMapping) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var input map[string]string
	if err := unmarshal(&input); err != nil {
		return err
	}

	result := make(map[string][]Upstream, len(input))

	for k, v := range input {
		var upstreams []Upstream

		for _, part := range strings.Split(v, ",") {
			upstream, err := ParseUpstream(strings.TrimSpace(part))
			if err != nil {
				return fmt.Errorf("can't convert upstream '%s': %w", strings.TrimSpace(part), err)
			}

			upstreams = append(upstreams, upstream)
		}

		result[k] = upstreams
	}

	c.Upstreams = result

	return nil
}

// UnmarshalYAML creates CustomDNSMapping from YAML
func (c *CustomDNSMapping) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var input map[string]string
	if err := unmarshal(&input); err != nil {
		return err
	}

	result := make(map[string][]net.IP, len(input))

	for k, v := range input {
		var ips []net.IP

		for _, part := range strings.Split(v, ",") {
			ip := net.ParseIP(strings.TrimSpace(part))
			if ip == nil {
				return fmt.Errorf("invalid IP address '%s'", part)
			}

			ips = append(ips, ip)
		}

		result[k] = ips
	}

	c.HostIPs = result

	return nil
}

func (c *QType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var input string
	if err := unmarshal(&input); err != nil {
		return err
	}

	t, found := dns.StringToType[input]
	if !found {
		types := make([]string, 0, len(dns.StringToType))
		for k := range dns.StringToType {
			types = append(types, k)
		}

		sort.Strings(types)

		return fmt.Errorf("unknown DNS query type: '%s'. Please use following types '%s'",
			input, strings.Join(types, ", "))
	}

	*c = QType(t)

	return nil
}

var validDomain = regexp.MustCompile(
	`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`)

// ParseUpstream creates new Upstream from passed string in format [net]:host[:port][/path][#commonname]
func ParseUpstream(upstream string) (Upstream, error) {
	var path string

	var port uint16

	commonName, upstream := extractCommonName(upstream)

	n, upstream := extractNet(upstream)

	path, upstream = extractPath(upstream)

	host, portString, err := net.SplitHostPort(upstream)

	// string contains host:port
	if err == nil {
		p, err := ConvertPort(portString)
		if err != nil {
			err = fmt.Errorf("can't convert port to number (1 - 65535) %w", err)

			return Upstream{}, err
		}

		port = p
	} else {
		// only host, use default port
		host = upstream
		port = netDefaultPort[n]

		// trim any IPv6 brackets
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
	}

	// validate hostname or ip
	if ip := net.ParseIP(host); ip == nil {
		// is not IP
		if !validDomain.MatchString(host) {
			return Upstream{}, fmt.Errorf("wrong host name '%s'", host)
		}
	}

	return Upstream{
		Net:        n,
		Host:       host,
		Port:       port,
		Path:       path,
		CommonName: commonName,
	}, nil
}

func extractCommonName(in string) (string, string) {
	upstream, cn, _ := strings.Cut(in, "#")

	return cn, upstream
}

func extractPath(in string) (path, upstream string) {
	slashIdx := strings.Index(in, "/")

	if slashIdx >= 0 {
		path = in[slashIdx:]
		upstream = in[:slashIdx]
	} else {
		upstream = in
	}

	return
}

func extractNet(upstream string) (NetProtocol, string) {
	tcpUDPPrefix := NetProtocolTcpUdp.String() + ":"
	if strings.HasPrefix(upstream, tcpUDPPrefix) {
		return NetProtocolTcpUdp, upstream[len(tcpUDPPrefix):]
	}

	tcpTLSPrefix := NetProtocolTcpTls.String() + ":"
	if strings.HasPrefix(upstream, tcpTLSPrefix) {
		return NetProtocolTcpTls, upstream[len(tcpTLSPrefix):]
	}

	httpsPrefix := NetProtocolHttps.String() + ":"
	if strings.HasPrefix(upstream, httpsPrefix) {
		return NetProtocolHttps, strings.TrimPrefix(upstream[len(httpsPrefix):], "//")
	}

	return NetProtocolTcpUdp, upstream
}

// Config main configuration
//
//nolint:maligned
type Config struct {
	Upstream            UpstreamConfig            `yaml:"upstream"`
	UpstreamTimeout     Duration                  `yaml:"upstreamTimeout" default:"2s"`
	ConnectIPVersion    IPVersion                 `yaml:"connectIPVersion"`
	CustomDNS           CustomDNSConfig           `yaml:"customDNS"`
	Conditional         ConditionalUpstreamConfig `yaml:"conditional"`
	Blocking            BlockingConfig            `yaml:"blocking"`
	ClientLookup        ClientLookupConfig        `yaml:"clientLookup"`
	Caching             CachingConfig             `yaml:"caching"`
	QueryLog            QueryLogConfig            `yaml:"queryLog"`
	Prometheus          PrometheusConfig          `yaml:"prometheus"`
	Redis               RedisConfig               `yaml:"redis"`
	Log                 log.Config                `yaml:"log"`
	Ports               PortsConfig               `yaml:"ports"`
	DoHUserAgent        string                    `yaml:"dohUserAgent"`
	MinTLSServeVer      string                    `yaml:"minTlsServeVersion" default:"1.2"`
	StartVerifyUpstream bool                      `yaml:"startVerifyUpstream" default:"false"`
	CertFile            string                    `koanf:"certFile"`
	KeyFile             string                    `koanf:"keyFile"`
	BootstrapDNS        BootstrapConfig           `koanf:"bootstrapDns"`
	HostsFile           HostsFileConfig           `koanf:"hostsFile"`
	FqdnOnly            bool                      `koanf:"fqdnOnly" default:"false"`
	Filtering           FilteringConfig           `koanf:"filtering"`
	Ede                 EdeConfig                 `koanf:"ede"`
	DisableIPv6         bool                      `yaml:"disableIPv6" default:"false"`
	// Deprecated
	LogLevel log.Level `yaml:"logLevel" default:"info"`
	// Deprecated
	LogFormat log.FormatType `yaml:"logFormat" default:"text"`
	// Deprecated
	LogPrivacy bool `yaml:"logPrivacy" default:"false"`
	// Deprecated
	LogTimestamp bool `yaml:"logTimestamp" default:"true"`
	// Deprecated
	DNSPorts ListenConfig `yaml:"port" default:"[\"53\"]"`
	// Deprecated
	HTTPPorts ListenConfig `yaml:"httpPort"`
	// Deprecated
	HTTPSPorts ListenConfig `yaml:"httpsPort"`
	// Deprecated
	TLSPorts ListenConfig `yaml:"tlsPort"`
}

type (
	BootstrapConfig bootstrapConfig // to avoid infinite recursion. See BootstrapConfig.UnmarshalYAML.
	bootstrapConfig struct {
		Upstream Upstream `yaml:"upstream"`
		IPs      []net.IP `yaml:"ips"`
	}
)

type PortsConfig struct {
	DNS   ListenConfig `yaml:"dns" default:"[\"53\"]"`
	HTTP  ListenConfig `yaml:"http"`
	HTTPS ListenConfig `yaml:"https"`
	TLS   ListenConfig `yaml:"tls"`
}

// PrometheusConfig contains the config values for prometheus
type PrometheusConfig struct {
	Enable bool   `koanf:"enable" default:"false"`
	Path   string `koanf:"path" default:"/metrics"`
}

// UpstreamConfig upstream server configuration
type UpstreamConfig struct {
	ExternalResolvers map[string][]Upstream `koanf:",remain"`
}

// RewriteConfig custom DNS configuration
type RewriteConfig struct {
	Rewrite          map[string]string `koanf:"rewrite"`
	FallbackUpstream bool              `koanf:"fallbackUpstream" default:"false"`
}

// CustomDNSConfig custom DNS configuration
type CustomDNSConfig struct {
	RewriteConfig       `koanf:",squash"`
	CustomTTL           Duration         `koanf:"customTTL" default:"1h"`
	Mapping             CustomDNSMapping `koanf:"mapping"`
	FilterUnmappedTypes bool             `koanf:"filterUnmappedTypes" default:"true"`
}

// CustomDNSMapping mapping for the custom DNS configuration
type CustomDNSMapping struct {
	HostIPs map[string][]net.IP `koanf:",remain"`
}

// ConditionalUpstreamConfig conditional upstream configuration
type ConditionalUpstreamConfig struct {
	RewriteConfig `koanf:",squash"`
	Mapping       ConditionalUpstreamMapping `koanf:"mapping"`
}

// ConditionalUpstreamMapping mapping for conditional configuration
type ConditionalUpstreamMapping struct {
	Upstreams map[string][]Upstream `koanf:",remain"`
}

// BlockingConfig configuration for query blocking
type BlockingConfig struct {
	BlackLists        map[string][]string `koanf:"blackLists"`
	WhiteLists        map[string][]string `koanf:"whiteLists"`
	ClientGroupsBlock map[string][]string `koanf:"clientGroupsBlock"`
	BlockType         string              `koanf:"blockType" default:"ZEROIP"`
	BlockTTL          Duration            `koanf:"blockTTL" default:"6h"`
	DownloadTimeout   Duration            `koanf:"downloadTimeout" default:"60s"`
	DownloadAttempts  uint                `koanf:"downloadAttempts" default:"3"`
	DownloadCooldown  Duration            `koanf:"downloadCooldown" default:"1s"`
	RefreshPeriod     Duration            `koanf:"refreshPeriod" default:"4h"`
	// Deprecated
	FailStartOnListError  bool              `koanf:"failStartOnListError" default:"false"`
	ProcessingConcurrency uint              `koanf:"processingConcurrency" default:"4"`
	StartStrategy         StartStrategyType `koanf:"startStrategy" default:"blocking"`
}

// ClientLookupConfig configuration for the client lookup
type ClientLookupConfig struct {
	ClientnameIPMapping map[string][]net.IP `koanf:"clients"`
	Upstream            Upstream            `koanf:"upstream"`
	SingleNameOrder     []uint              `koanf:"singleNameOrder"`
}

// CachingConfig configuration for domain caching
type CachingConfig struct {
	MinCachingTime        Duration `koanf:"minTime"`
	MaxCachingTime        Duration `koanf:"maxTime"`
	CacheTimeNegative     Duration `koanf:"cacheTimeNegative" default:"30m"`
	MaxItemsCount         int      `koanf:"maxItemsCount"`
	Prefetching           bool     `koanf:"prefetching"`
	PrefetchExpires       Duration `koanf:"prefetchExpires" default:"2h"`
	PrefetchThreshold     int      `koanf:"prefetchThreshold" default:"5"`
	PrefetchMaxItemsCount int      `koanf:"prefetchMaxItemsCount"`
}

// QueryLogConfig configuration for the query logging
type QueryLogConfig struct {
	Target           string          `yaml:"target"`
	Type             QueryLogType    `yaml:"type"`
	LogRetentionDays uint64          `yaml:"logRetentionDays"`
	CreationAttempts int             `yaml:"creationAttempts" default:"3"`
	CreationCooldown Duration        `yaml:"creationCooldown" default:"2s"`
	Fields           []QueryLogField `yaml:"fields"`
}

// RedisConfig configuration for the redis connection
type RedisConfig struct {
	Address            string   `yaml:"address"`
	Username           string   `yaml:"username" default:""`
	Password           string   `yaml:"password" default:""`
	Database           int      `yaml:"database" default:"0"`
	Required           bool     `yaml:"required" default:"false"`
	ConnectionAttempts int      `yaml:"connectionAttempts" default:"3"`
	ConnectionCooldown Duration `yaml:"connectionCooldown" default:"1s"`
	SentinelUsername   string   `yaml:"sentinelUsername" default:""`
	SentinelPassword   string   `yaml:"sentinelPassword" default:""`
	SentinelAddresses  []string `yaml:"sentinelAddresses"`
}

type HostsFileConfig struct {
	Filepath       string   `koanf:"filePath"`
	HostsTTL       Duration `koanf:"hostsTTL" default:"1h"`
	RefreshPeriod  Duration `koanf:"refreshPeriod" default:"1h"`
	FilterLoopback bool     `koanf:"filterLoopback"`
}

type FilteringConfig struct {
	QueryTypes QTypeSet `koanf:"queryTypes"`
}

type EdeConfig struct {
	Enable bool `koanf:"enable" default:"false"`
}

//nolint:gochecknoglobals
var (
	config  = &Config{}
	cfgLock sync.RWMutex
)

// LoadConfig creates new config from YAML file or a directory containing YAML files
func LoadConfig(path string, mandatory bool) (*Config, error) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	cfg := Config{}
	if err := defaults.Set(&cfg); err != nil {
		return nil, fmt.Errorf("can't apply default values: %w", err)
	}

	k := koanf.New("_")

	fs, err := os.Stat(path)
	if err == nil && fs.IsDir() {
		if err := loadDir(path, k); err != nil {
			return nil, fmt.Errorf("can't read config folder: %w", err)
		}
	} else if err == nil {
		if err := loadFile(k, path); err != nil {
			return nil, fmt.Errorf("can't read config file: %w", err)
		}
	}

	err = loadEnvironment(k)
	if err != nil {
		return nil, fmt.Errorf("can't read environment config: %w", err)
	}

	if err := unmarshalKoanf(k, &cfg); err != nil {
		return nil, err
	}

	validateConfig(&cfg)

	config = &cfg

	return &cfg, nil
}

// isRegularFile follows symlinks, so the result is `true` for a symlink to a regular file.
func isRegularFile(path string) (bool, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	isRegular := stat.Mode()&os.ModeType == 0

	return isRegular, nil
}

func validateConfig(cfg *Config) {
	if cfg.DisableIPv6 {
		log.Log().Warnf("'disableIPv6' is deprecated. Please use 'filtering.queryTypes' with 'AAAA' instead.")

		cfg.Filtering.QueryTypes.Insert(dns.Type(dns.TypeAAAA))
	}

	if cfg.Blocking.FailStartOnListError {
		log.Log().Warnf("'blocking.failStartOnListError' is deprecated. Please use 'blocking.startStrategy'" +
			" with 'failOnError' instead.")

		if cfg.Blocking.StartStrategy == StartStrategyTypeBlocking {
			cfg.Blocking.StartStrategy = StartStrategyTypeFailOnError
		} else if cfg.Blocking.StartStrategy == StartStrategyTypeFast {
			log.Log().Warnf("'blocking.startStrategy' with 'fast' will ignore 'blocking.failStartOnListError'.")
		}
	}

	fixDeprecatedLog(cfg)

	fixDeprecatedPorts(cfg)
}

// fixDeprecatedLog ensures backwards compatibility for logging options
func fixDeprecatedLog(cfg *Config) {
	if cfg.LogLevel != log.LevelInfo && cfg.Log.Level == log.LevelInfo {
		log.Log().Warnf("'logLevel' is deprecated. Please use 'log.level' instead.")

		cfg.Log.Level = cfg.LogLevel
	}

	if cfg.LogFormat != log.FormatTypeText && cfg.Log.Format == log.FormatTypeText {
		log.Log().Warnf("'logFormat' is deprecated. Please use 'log.format' instead.")

		cfg.Log.Format = cfg.LogFormat
	}

	if cfg.LogPrivacy && !cfg.Log.Privacy {
		log.Log().Warnf("'logPrivacy' is deprecated. Please use 'log.privacy' instead.")

		cfg.Log.Privacy = cfg.LogPrivacy
	}

	if !cfg.LogTimestamp && cfg.Log.Timestamp {
		log.Log().Warnf("'logTimestamp' is deprecated. Please use 'log.timestamp' instead.")

		cfg.Log.Timestamp = cfg.LogTimestamp
	}
}

// fixDeprecatedPorts ensures backwards compatibility for ports options
func fixDeprecatedPorts(cfg *Config) {
	defaultDNSPort := ListenConfig([]string{"53"})
	if (len(cfg.DNSPorts) > 1 || (len(cfg.DNSPorts) == 1 && cfg.DNSPorts[0] != defaultDNSPort[0])) &&
		(len(cfg.Ports.DNS) == 1 && cfg.Ports.DNS[0] == defaultDNSPort[0]) {
		log.Log().Warnf("'port' is deprecated. Please use 'ports.dns' instead.")

		cfg.Ports.DNS = cfg.DNSPorts
	}

	if len(cfg.HTTPPorts) > 0 && len(cfg.Ports.HTTP) == 0 {
		log.Log().Warnf("'httpPort' is deprecated. Please use 'ports.http' instead.")

		cfg.Ports.HTTP = cfg.HTTPPorts
	}

	if len(cfg.HTTPSPorts) > 0 && len(cfg.Ports.HTTPS) == 0 {
		log.Log().Warnf("'httpsPort' is deprecated. Please use 'ports.https' instead.")

		cfg.Ports.HTTPS = cfg.HTTPSPorts
	}

	if len(cfg.TLSPorts) > 0 && len(cfg.Ports.TLS) == 0 {
		log.Log().Warnf("'tlsPort' is deprecated. Please use 'ports.tls' instead.")

		cfg.Ports.TLS = cfg.TLSPorts
	}
}

// GetConfig returns the current config
func GetConfig() *Config {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	return config
}

// ConvertPort converts string representation into a valid port (0 - 65535)
func ConvertPort(in string) (uint16, error) {
	const (
		base    = 10
		bitSize = 16
	)

	p, err := strconv.ParseUint(strings.TrimSpace(in), base, bitSize)
	if err != nil {
		return 0, err
	}

	return uint16(p), nil
}
