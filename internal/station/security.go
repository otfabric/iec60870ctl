// SPDX-License-Identifier: MIT

package station

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-iec104/sec"
	"github.com/otfabric/iec104ctl/internal/types"
)

// newEndpoint makes the association of a controlling station, named by its AIM, for
// server.WithSecurity: every controlling station has the identity, trust and roles of the
// options.
func (st *Station) newEndpoint(group string, aim uint16) (*sec.Endpoint, error) {
	ep, err := st.endpoint(group, aim)
	if err != nil {
		st.event(types.ServerEvent{Event: "security-refused", Detail: fmt.Sprintf("AIM %d: %v", aim, err)})
		return nil, err
	}
	return ep, nil
}

func (st *Station) endpoint(group string, aim uint16) (*sec.Endpoint, error) {
	s := st.opts.Security
	var ep *sec.Endpoint
	ep, err := sec.New(sec.Config{
		Role:          sec.Controlled,
		AssociationID: s.ID,
		Identity:      s.Identity,
		Trust:         s.Trust,
		Roles:         s.Roles,
		Params:        st.opts.ASDU,
		CommonAddr:    st.ca,
		OnEvent:       func(e sec.Event) { st.securityEvent(group, aim, ep, e) },
	})
	return ep, err
}

// securityEvent logs what an operator needs to know of the security events of the
// association of aim: that it was established, that it was refused, and requests the
// roles did not permit.
func (st *Station) securityEvent(group string, aim uint16, ep *sec.Endpoint, e sec.Event) {
	who := fmt.Sprintf("AIM %d", aim)
	if group != "default" {
		who = fmt.Sprintf("group %s, AIM %d", group, aim)
	}
	why := ""
	if e.Err != nil {
		why = ": " + strings.TrimPrefix(e.Err.Error(), "sec: ")
	}
	switch e.Stat {
	case sec.StatSuccessfulAssociations:
		detail := who
		if ep != nil {
			s := ep.Status()
			if s.Remote != nil {
				detail += fmt.Sprintf(", %q", s.Remote.Subject.CommonName)
			}
			detail += fmt.Sprintf(", AIS %d, MAC %s", s.AIS, s.MAC)
		}
		st.event(types.ServerEvent{Event: "secured", Detail: detail})
	case sec.StatAssociationFailures, sec.StatRemoteCertificateFailures, sec.StatRemoteCertificateExpirations,
		sec.StatRemoteAuthorizationFailures, sec.StatProtocolInformationErrors, sec.StatKeyAuthAlgorithmFailures,
		sec.StatKeyWrapAlgorithmFailures, sec.StatDataProtectionAlgorithmFailures, sec.StatKeyAuthenticationErrors:
		st.event(types.ServerEvent{Event: "security-refused", Detail: fmt.Sprintf("%s, %s%s", who, mnemonic(e), why)})
	case sec.StatOperationAuthorizationFailures:
		st.event(types.ServerEvent{Event: "refused", Detail: fmt.Sprintf("%s%s", who, why)})
	default:
	}
}

func mnemonic(e sec.Event) string {
	if e.Mnemonic != "" {
		return e.Mnemonic
	}
	return e.Stat.String()
}
