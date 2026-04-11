package dhcpsrv

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"IBootTime/internal/config"
	"IBootTime/internal/logger"
	"IBootTime/internal/session"
)

const (
	dhcpServerPort = 67
	dhcpClientPort = 68

	// DHCP Message Types
	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5

	// DHCP Options
	optSubnetMask   = 1
	optRouter       = 3
	optDNS          = 6
	optHostname     = 12
	optDomainName   = 15
	optBroadcast    = 28
	optRequestedIP  = 50
	optLeaseTime    = 51
	optMessageType  = 53
	optServerID     = 54
	optParamRequest = 55
	optVendorClass  = 60
	optTFTPServer   = 66
	optBootfile     = 67
	optUserClass    = 77
	optClientArch   = 93
	optIPXEEncap    = 175 // iPXE encapsulated options — definitive iPXE detection
	optEnd          = 255
)

type Server struct {
	mu         sync.Mutex
	conn       *net.UDPConn
	proxyConn  *net.UDPConn // port 4011 for ProxyDHCP
	sendConn   *net.UDPConn // dedicated send socket bound to server IP (FIX: broadcast routing)
	cfg        *config.Config
	log        *logger.Logger
	sessions   *session.Manager
	serverIP   net.IP
	bcastIP    net.IP // subnet-directed broadcast (e.g. 192.168.1.255)
	cancelFunc context.CancelFunc
	running    bool

	// Dedup: both conn and sendConn receive same broadcast packet
	seenXID sync.Map // "xid:mac:type" -> true

	// Track MACs that sent PXE DISCOVER so we process their REQUEST
	// even if it lacks vendor class "PXEClient" (common in UEFI).
	// Stores time.Time of last PXE interaction; expires after pxeClientTTL
	// to avoid interfering with the booted OS's regular DHCP.
	pxeClients sync.Map // MAC -> time.Time
}

func New(cfg *config.Config, serverIP string, log *logger.Logger, sessions *session.Manager) *Server {
	sip := net.ParseIP(serverIP).To4()

	// Calculate subnet-directed broadcast (assume /24 — router handles real subnet)
	mask := net.IP{255, 255, 255, 0}
	bcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		bcast[i] = sip[i] | ^mask[i]
	}

	return &Server{
		cfg:      cfg,
		log:      log,
		sessions: sessions,
		serverIP: sip,
		bcastIP:  bcast,
	}
}

func (s *Server) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	// Main DHCP listener on port 67 — bind to 0.0.0.0 to receive broadcasts
	addr := &net.UDPAddr{Port: dhcpServerPort, IP: net.IPv4zero}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		cancel()
		return fmt.Errorf("DHCP listen: %w", err)
	}
	s.conn = conn

	// Dedicated send socket bound to SERVER_IP:67.
	sendAddr := &net.UDPAddr{Port: dhcpServerPort, IP: s.serverIP}
	sendConn, err := net.ListenUDP("udp4", sendAddr)
	if err != nil {
		s.log.Warn("DHCP", "Cannot create send socket on %s:%d: %v (falling back to main socket)", s.serverIP, dhcpServerPort, err)
		s.sendConn = conn // fallback
	} else {
		s.sendConn = sendConn
		s.log.Info("DHCP", "Send socket bound to %s:%d (correct NIC + source port 67)", s.serverIP, dhcpServerPort)
	}

	s.log.Info("DHCP", "Starting proxy PXE server on :%d serverIP=%s bcast=%s",
		dhcpServerPort, s.serverIP, s.bcastIP)

	// ProxyDHCP listener on port 4011
	proxyAddr := &net.UDPAddr{Port: 4011, IP: net.IPv4zero}
	proxyConn, err := net.ListenUDP("udp4", proxyAddr)
	if err != nil {
		s.log.Warn("DHCP", "Cannot bind ProxyDHCP port 4011: %v", err)
	} else {
		s.proxyConn = proxyConn
		s.log.Info("DHCP", "ProxyDHCP listening on :4011")
	}

	go func() {
		<-ctx.Done()
		conn.Close()
		if s.sendConn != nil && s.sendConn != conn {
			s.sendConn.Close()
		}
		if s.proxyConn != nil {
			s.proxyConn.Close()
		}
	}()

	go s.listen(ctx, s.conn, "67")
	if s.sendConn != nil && s.sendConn != s.conn {
		go s.listen(ctx, s.sendConn, "67-unicast")
	}
	if s.proxyConn != nil {
		go s.listen(ctx, s.proxyConn, "4011")
	}

	return nil
}

func (s *Server) Stop() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.log.Info("DHCP", "DHCP server stopped")
}

func (s *Server) listen(ctx context.Context, conn *net.UDPConn, portLabel string) {
	buf := make([]byte, 1500)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Error("DHCP", "Read error on port %s: %v", portLabel, err)
			continue
		}

		if n < 240 {
			continue
		}

		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go s.handlePacket(pkt, remoteAddr, conn, portLabel)
	}
}

func (s *Server) handlePacket(data []byte, remoteAddr *net.UDPAddr, conn *net.UDPConn, portLabel string) {
	if data[0] != 1 { // Must be BOOTREQUEST
		return
	}

	mac := net.HardwareAddr(data[28:34]).String()
	xid := data[4:8]
	options := parseOptions(data[240:])

	msgType, ok := options[optMessageType]
	if !ok || len(msgType) == 0 {
		return
	}

	// Dedup: both conn and sendConn may receive same broadcast
	if portLabel != "4011" {
		xidKey := fmt.Sprintf("%x:%s:%d", xid, mac, msgType[0])
		if _, loaded := s.seenXID.LoadOrStore(xidKey, true); loaded {
			return
		}
		go func() {
			time.Sleep(2 * time.Second)
			s.seenXID.Delete(xidKey)
		}()
	}

	// Detect architecture from Option 93 (same as iVentoy)
	arch := "BIOS"
	if archOpt, ok := options[optClientArch]; ok && len(archOpt) >= 2 {
		archCode := (int(archOpt[0]) << 8) | int(archOpt[1])
		switch archCode {
		case 0:
			arch = "BIOS"
		case 6:
			arch = "UEFI-x86"
		case 7, 8, 9:
			arch = "UEFI-x64"
		case 10:
			arch = "UEFI-ARM32"
		case 11:
			arch = "UEFI-ARM64"
		}
	}

	// Vendor/User class
	vcStr := ""
	if vc, ok := options[optVendorClass]; ok {
		vcStr = string(vc)
	}
	ucStr := ""
	if uc, ok := options[optUserClass]; ok {
		ucStr = string(uc)
	}

	// iPXE detection (iVentoy style: opt175 → user-class → vendor-class)
	isIPXE := false
	hasHTTP := false
	if ipxeData, ok := options[optIPXEEncap]; ok {
		isIPXE = true
		subOpts := parseOptions(ipxeData)
		_, hasHTTP = subOpts[19]
		s.log.Info("DHCP", "[%s] %s from %s [%s] iPXE=true HTTP=%v (opt175 subs: %v)",
			portLabel, msgTypeStr(msgType[0]), mac, arch, hasHTTP, subOptionKeys(subOpts))
	} else {
		if strings.Contains(ucStr, "iPXE") || strings.Contains(vcStr, "iPXE") {
			isIPXE = true
		}
		s.log.Info("DHCP", "[%s] %s from %s [%s] iPXE=%v VC=%q UC=%q",
			portLabel, msgTypeStr(msgType[0]), mac, arch, isIPXE, vcStr, ucStr)
	}

	// Only handle PXE clients (iVentoy ignores non-PXE traffic).
	// opt93 (Client System Architecture) is sent by ALL PXE ROMs but
	// NEVER by regular OS DHCP clients — most reliable PXE indicator.
	_, hasArchOpt := options[optClientArch]
	isPXEClient := isIPXE || strings.HasPrefix(vcStr, "PXEClient") || hasArchOpt

	if isPXEClient {
		s.pxeClients.Store(mac, time.Now())
	} else if msgType[0] == dhcpRequest {
		// Only use memory fallback for REQUEST (UEFI often omits vendor
		// class in REQUEST). For DISCOVER, require explicit PXE indicators
		// so we don't interfere with the booted OS's regular DHCP.
		const pxeClientTTL = 10 * time.Second
		if lastSeen, known := s.pxeClients.Load(mac); known {
			if time.Since(lastSeen.(time.Time)) < pxeClientTTL {
				isPXEClient = true
			} else {
				s.pxeClients.Delete(mac)
				s.log.Info("DHCP", "PXE client TTL expired for %s (booted OS?)", mac)
			}
		}
	}

	if !isPXEClient {
		return
	}

	// Register client session
	clientIP := remoteAddr.IP.String()
	if ciaddr := net.IP(data[12:16]).To4(); ciaddr != nil && !ciaddr.Equal(net.IPv4zero) {
		clientIP = ciaddr.String()
	} else if reqIP, ok := options[optRequestedIP]; ok && len(reqIP) == 4 {
		clientIP = net.IP(reqIP).String()
	}
	s.sessions.Register(mac, clientIP, arch)

	// =================================================================
	// PURE PROXY PXE MODE — we NEVER assign IPs.
	// The router handles all IP assignment.
	// We only respond with PXE boot info (siaddr + filename).
	// =================================================================

	// --- Port 4011: ProxyDHCP boot server ---
	// Some PXE ROMs contact us here after receiving our OFFER on port 67.
	if portLabel == "4011" {
		s.log.Info("DHCP", "ProxyDHCP :4011 %s from %s → boot ACK", msgTypeStr(msgType[0]), mac)
		s.sendBootResponse(dhcpAck, data, xid, mac, arch, isIPXE, hasHTTP, conn, remoteAddr, nil)
		return
	}

	// --- Port 67: Proxy PXE responses ---
	//
	// Strategy varies by client type:
	//
	//   iPXE clients:     Immediate proxy OFFER (iPXE handles ProxyDHCP correctly)
	//   BIOS PXE ROM:     Delayed proxy OFFER (200ms, let router respond first)
	//   UEFI PXE ROM:     NO proxy OFFER — many UEFI firmwares get confused by
	//                     yiaddr=0 OFFERs and restart DISCOVER. Boot info
	//                     delivered only via piggyback ACK on REQUEST.
	//
	//   All REQUEST:      Piggyback ACK (client's IP from router + boot info)
	//
	isUEFI := strings.HasPrefix(arch, "UEFI")

	switch msgType[0] {
	case dhcpDiscover:
		if isIPXE {
			// iPXE: respond immediately — it handles ProxyDHCP correctly
			// and has a short OFFER collection window.
			s.log.Info("DHCP", "Proxy OFFER for iPXE %s [%s] (immediate)", mac, arch)
			s.sendBootResponse(dhcpOffer, data, xid, mac, arch, isIPXE, hasHTTP, nil, nil, nil)
		} else if isUEFI {
			// UEFI PXE ROM: do NOT send proxy OFFER.
			// Many UEFI implementations treat yiaddr=0 OFFER as broken DHCP,
			// restart DISCOVER, and never get an IP. Boot info will be
			// delivered via piggyback ACK when we see the REQUEST.
			s.log.Info("DHCP", "UEFI DISCOVER from %s — skipping OFFER (will piggyback on REQUEST)", mac)
		} else {
			// BIOS PXE ROM: delayed proxy OFFER.
			// The 200ms delay lets the router's real OFFER arrive first.
			// BIOS PXE ROMs need to see our OFFER to know PXE boot is available.
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			xidCopy := make([]byte, 4)
			copy(xidCopy, xid)
			go func() {
				time.Sleep(200 * time.Millisecond)
				s.log.Info("DHCP", "Proxy OFFER for BIOS %s (delayed 200ms)", mac)
				s.sendBootResponse(dhcpOffer, dataCopy, xidCopy, mac, arch, isIPXE, hasHTTP, nil, nil, nil)
			}()
		}

	case dhcpRequest:
		// Piggyback ACK for ALL clients: send our own ACK with boot info.
		//   - yiaddr = same IP the router gives (no conflict)
		//   - No option 54 (server ID) so PXE ROM won't reject it
		//   - Boot info (siaddr, filename, opt66, opt67) gets delivered
		//   - For UEFI clients this is the PRIMARY delivery of boot info
		var reqIP net.IP
		if rip, ok := options[optRequestedIP]; ok && len(rip) == 4 {
			reqIP = net.IP(rip).To4()
		}
		if reqIP == nil || reqIP.Equal(net.IPv4zero) {
			if ciaddr := net.IP(data[12:16]).To4(); !ciaddr.Equal(net.IPv4zero) {
				reqIP = ciaddr
			}
		}
		if reqIP != nil && !reqIP.Equal(net.IPv4zero) {
			s.log.Info("DHCP", "Piggyback ACK for %s: IP=%s + boot info", mac, reqIP)
			s.sendBootResponse(dhcpAck, data, xid, mac, arch, isIPXE, hasHTTP, nil, nil, reqIP)
		} else {
			s.log.Warn("DHCP", "REQUEST from %s but no IP found (opt50 and ciaddr both empty)", mac)
		}
	}
}

// sendBootResponse builds and sends a proxy PXE DHCP response.
//
// Two modes:
//   - Proxy OFFER (piggybackIP=nil): yiaddr=0, boot info only
//   - Piggyback ACK (piggybackIP!=nil): yiaddr=client's IP from router,
//     boot info included, option 54 OMITTED to avoid rejection.
//
// We NEVER assign IPs — the router handles that.
func (s *Server) sendBootResponse(msgType byte, origData, xid []byte, mac, arch string, isIPXE, hasHTTP bool, viaCon *net.UDPConn, clientAddr *net.UDPAddr, piggybackIP net.IP) {
	response := make([]byte, 1024)

	// --- BOOTP Header ---
	response[0] = 2           // BOOTREPLY
	response[1] = origData[1] // htype (Ethernet=1)
	response[2] = origData[2] // hlen  (MAC=6)
	response[3] = origData[3] // hops
	copy(response[4:8], xid)
	copy(response[8:10], origData[8:10])   // secs
	copy(response[10:12], origData[10:12]) // flags: preserve client's broadcast bit

	// ciaddr — copy from request
	copy(response[12:16], origData[12:16])

	// yiaddr — we never assign IPs
	if piggybackIP != nil {
		// Piggyback: echo the IP the client got from the router
		copy(response[16:20], piggybackIP.To4())
		s.log.Info("DHCP", "  yiaddr=%s (piggyback from router) for %s", piggybackIP, mac)
	}
	// else: proxy OFFER → yiaddr stays 0 (router assigns IP)

	// siaddr — our TFTP/next-server IP
	copy(response[20:24], s.serverIP.To4())
	// giaddr — relay agent (copy from request)
	copy(response[24:28], origData[24:28])
	// chaddr — client hardware address
	copy(response[28:44], origData[28:44])

	// sname — server hostname (our IP as string)
	copy(response[44:108], []byte(s.serverIP.String()))

	// file — boot filename (BOOTP legacy field)
	bootFile := s.getBootFilename(arch, isIPXE, hasHTTP)
	copy(response[108:236], []byte(bootFile))

	// Magic cookie
	copy(response[236:240], []byte{99, 130, 83, 99})

	// --- DHCP Options ---
	offset := 240

	// Option 53: Message Type
	offset += putOption(response[offset:], optMessageType, []byte{msgType})

	// Option 54: Server Identifier
	// OMIT in piggyback mode — including our IP as server ID when the
	// client requested from the router causes some PXE ROMs to reject it.
	if piggybackIP == nil {
		offset += putOption(response[offset:], optServerID, s.serverIP.To4())
	}

	// Option 60: Vendor Class Identifier
	offset += putOption(response[offset:], optVendorClass, []byte("PXEClient"))

	// Option 43: PXE Vendor-Specific (for PXE ROM, not iPXE)
	// PXEBS_SKIP tells PXE ROM to download boot file directly from siaddr
	if !isIPXE {
		vendorOpts := buildPXEVendorOptions(s.serverIP)
		offset += putOption(response[offset:], 43, vendorOpts)
	}

	// Option 66: TFTP Server Name
	offset += putOption(response[offset:], optTFTPServer, []byte(s.serverIP.String()))
	// Option 67: Boot Filename
	offset += putOption(response[offset:], optBootfile, []byte(bootFile))

	// No network config (subnet, gateway, DNS, lease) — router handles all of that

	// End option
	response[offset] = optEnd
	offset++

	// Pad to minimum BOOTP/DHCP packet size
	if offset < 300 {
		offset = 300
	}

	isPB := piggybackIP != nil
	s.log.Info("DHCP", ">> %s to %s: file=%q siaddr=%s piggyback=%v (%d bytes)",
		msgTypeStr(msgType), mac, bootFile, s.serverIP, isPB, offset)

	// --- Send Packet ---
	if viaCon != nil && clientAddr != nil {
		// Unicast reply (port 4011 ProxyDHCP response)
		if _, err := viaCon.WriteToUDP(response[:offset], clientAddr); err != nil {
			s.log.Error("DHCP", "  Unicast to %s (%s) failed: %v", mac, clientAddr, err)
		} else {
			s.log.Info("DHCP", "  Sent unicast -> %s", clientAddr)
		}
		return
	}

	// Broadcast reply (port 67 responses)
	sent := false
	dst1 := &net.UDPAddr{IP: s.bcastIP, Port: dhcpClientPort}
	dst2 := &net.UDPAddr{IP: net.IPv4bcast, Port: dhcpClientPort}

	// sendConn (bound to serverIP:67) — correct NIC routing
	if s.sendConn != nil {
		if _, err := s.sendConn.WriteToUDP(response[:offset], dst1); err == nil {
			sent = true
		}
		s.sendConn.WriteToUDP(response[:offset], dst2)
	}

	// conn (bound to 0.0.0.0:67) — fallback via default route
	s.conn.WriteToUDP(response[:offset], dst1)
	s.conn.WriteToUDP(response[:offset], dst2)

	if sent {
		s.log.Info("DHCP", "  Broadcast via NIC %s -> %s:%d", s.serverIP, s.bcastIP, dhcpClientPort)
	} else {
		s.log.Warn("DHCP", "  sendConn failed — broadcast via 0.0.0.0 only")
	}
}

func (s *Server) getBootFilename(arch string, isIPXE, hasHTTP bool) string {
	bootProto := s.cfg.GetBootProtocol()

	switch bootProto {
	case config.BootProtocolGRUB:
		if isIPXE && hasHTTP {
			url := fmt.Sprintf("http://%s:%d/boot.ipxe", s.serverIP.String(), s.cfg.HTTPPort)
			s.log.Info("DHCP", "  GRUB mode + iPXE+HTTP → fallback iPXE script: %s", url)
			return url
		}
		switch {
		case strings.Contains(arch, "UEFI"):
			s.log.Info("DHCP", "  GRUB mode (UEFI %s) → grubx64.efi (via TFTP)", arch)
			return "grubx64.efi"
		default:
			s.log.Info("DHCP", "  GRUB mode (BIOS) → grub2pxe (via TFTP)")
			return "grub2pxe"
		}

	case config.BootProtocolUndionly:
		if isIPXE {
			if hasHTTP {
				url := fmt.Sprintf("http://%s:%d/boot.ipxe", s.serverIP.String(), s.cfg.HTTPPort)
				s.log.Info("DHCP", "  Undionly mode + iPXE+HTTP → shell script: %s", url)
				return url
			}
			s.log.Info("DHCP", "  Undionly mode + iPXE → boot.ipxe (via TFTP, shell only)")
			return "boot.ipxe"
		}
		switch {
		case strings.Contains(arch, "UEFI"):
			s.log.Info("DHCP", "  Undionly mode (UEFI %s) → snp.efi (via TFTP)", arch)
			return "snp.efi"
		default:
			s.log.Info("DHCP", "  Undionly mode (BIOS) → undionly.kpxe (via TFTP)")
			return "undionly.kpxe"
		}

	default: // BootProtocolIPXE
		if isIPXE && hasHTTP {
			url := fmt.Sprintf("http://%s:%d/boot.ipxe", s.serverIP.String(), s.cfg.HTTPPort)
			s.log.Info("DHCP", "  STEP 2 (Full iPXE+HTTP) -> boot file: %s", url)
			return url
		}

		if isIPXE {
			// iPXE without HTTP (e.g. VirtualBox built-in iPXE): serve
			// full menu script via TFTP instead of the binary (which
			// would cause an infinite boot loop).
			s.log.Info("DHCP", "  iPXE without HTTP -> boot.ipxe (full menu via TFTP)")
			return "boot.ipxe"
		}

		var file string
		switch {
		case strings.Contains(arch, "UEFI"):
			file = "ipxe.efi"
		default:
			file = "undionly.kpxe"
		}
		s.log.Info("DHCP", "  STEP 1 (PXE ROM %s) -> boot file: %s (via TFTP)", arch, file)
		return file
	}
}

// buildPXEVendorOptions builds Option 43 sub-options for PXE boot.
// Uses PXEBS_SKIP (0x08) discovery control so the PXE ROM downloads
// the boot file directly from siaddr via TFTP without needing port 4011.
// This is the most compatible approach across all PXE ROM implementations.
// Also includes a boot server list and menu for PXE ROMs that need them.
func buildPXEVendorOptions(serverIP net.IP) []byte {
	var buf []byte

	// Sub-option 6: PXE Discovery Control
	// 0x08 = PXE_DISCOVERY_SKIP: skip boot server discovery, use filename directly
	// This eliminates the need for port 4011 communication
	buf = append(buf, 6, 1, 0x08)

	// Sub-option 8: Boot Server List (type 0 = generic, 1 server)
	bootServers := []byte{0x00, 0x00, 1}
	bootServers = append(bootServers, serverIP.To4()...)
	buf = append(buf, 8, byte(len(bootServers)))
	buf = append(buf, bootServers...)

	// Sub-option 9: Boot Menu
	desc := "IBootTime"
	bootMenu := []byte{0x00, 0x00, byte(len(desc))}
	bootMenu = append(bootMenu, []byte(desc)...)
	buf = append(buf, 9, byte(len(bootMenu)))
	buf = append(buf, bootMenu...)

	// Sub-option 10: Menu Prompt (timeout=0 = no prompt, boot immediately)
	menuPrompt := []byte{0}
	menuPrompt = append(menuPrompt, []byte("IBootTime")...)
	buf = append(buf, 10, byte(len(menuPrompt)))
	buf = append(buf, menuPrompt...)

	// End marker
	buf = append(buf, 0xFF)

	return buf
}

func putOption(buf []byte, code byte, data []byte) int {
	buf[0] = code
	buf[1] = byte(len(data))
	copy(buf[2:], data)
	return 2 + len(data)
}

func (s *Server) SetServerIP(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverIP = net.ParseIP(ip)
}

func parseOptions(data []byte) map[byte][]byte {
	opts := make(map[byte][]byte)
	i := 0
	for i < len(data) {
		if data[i] == optEnd {
			break
		}
		if data[i] == 0 {
			i++
			continue
		}
		if i+1 >= len(data) {
			break
		}
		code := data[i]
		length := int(data[i+1])
		i += 2
		if i+length > len(data) {
			break
		}
		val := make([]byte, length)
		copy(val, data[i:i+length])
		opts[code] = val
		i += length
	}
	return opts
}


func subOptionKeys(opts map[byte][]byte) []int {
	keys := make([]int, 0, len(opts))
	for k := range opts {
		keys = append(keys, int(k))
	}
	return keys
}

func msgTypeStr(t byte) string {
	switch t {
	case dhcpDiscover:
		return "DISCOVER"
	case dhcpOffer:
		return "OFFER"
	case dhcpRequest:
		return "REQUEST"
	case dhcpAck:
		return "ACK"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}