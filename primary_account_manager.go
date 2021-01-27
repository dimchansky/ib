package ib

import (
	"fmt"
)

// PrimaryAccountManager tracks the primary IB account's values and portfolio,
// along with all FA sub-accounts. FA accounts may also consider using
// AdvisorAccountManager, although the latter will not report position P&Ls.
type PrimaryAccountManager struct {
	Manager
	id          int64
	accountCode []string
	unsubscribe string
	values      map[AccountValueKey]AccountValue
	portfolio   map[PortfolioValueKey]PortfolioValue
}

// NewPrimaryAccountManager .
func NewPrimaryAccountManager(e *Engine) (*PrimaryAccountManager, error) {
	am, err := NewAbstractManager(e)
	if err != nil {
		return nil, err
	}

	p := &PrimaryAccountManager{Manager: am,
		id:        UnmatchedReplyID,
		values:    map[AccountValueKey]AccountValue{},
		portfolio: map[PortfolioValueKey]PortfolioValue{},
	}

	go p.am().StartMainLoop(p.preLoop, p.receive, p.preDestroy)
	return p, nil
}

func (p *PrimaryAccountManager) am() *AbstractManager {
	return (p.Manager).(*AbstractManager)
}

func (p *PrimaryAccountManager) engine() *Engine {
	return p.am().Engine()
}

func (p *PrimaryAccountManager) replyCh() chan Reply {
	return p.am().ReplyCh()
}

func (p *PrimaryAccountManager) preLoop() error {
	eng := p.engine()
	rc := p.replyCh()

	eng.Subscribe(rc, p.id)

	// To address if being run under an FA account, request our accounts
	// (the 321 warning-level error will be ignored for non-FA accounts)
	return eng.Send(&RequestManagedAccounts{})
}

func (p *PrimaryAccountManager) receive(r Reply) (UpdateStatus, error) {
	switch r.(type) {
	case *ErrorMessage:
		r := r.(*ErrorMessage)
		if r.SeverityWarning() {
			return UpdateFalse, nil
		}
		return UpdateFalse, r
	case *AccountDownloadEnd:
		finished, err := p.nextAccount()
		if err != nil {
			return UpdateFalse, err
		}
		if finished {
			return UpdateTrue, nil
		}
		return UpdateFalse, nil
	case *NextValidID:
		return UpdateFalse, nil
	case *AccountUpdateTime:
		return UpdateFalse, nil
	case *AccountValue:
		t := r.(*AccountValue)
		p.values[t.Key] = *t
		return UpdateFalse, nil
	case *PortfolioValue:
		t := r.(*PortfolioValue)
		p.portfolio[t.Key] = *t
		return UpdateFalse, nil
	case *ManagedAccounts:
		t := r.(*ManagedAccounts)
		if len(t.AccountsList) == 0 {
			return UpdateFalse, fmt.Errorf("goib: account manager found no accounts")
		}

		// Refine the request so we don't block if an FA login
		p.accountCode = t.AccountsList
		p.nextAccount()
		return UpdateFalse, nil
	}
	return UpdateFalse, fmt.Errorf("Unexpected type %v", r)
}

// nextAccount requests the next FA account, unsubscribing from any previous
// request and returning true if no more accounts are remaining.
func (p *PrimaryAccountManager) nextAccount() (bool, error) {
	eng := p.engine()

	if p.unsubscribe != "" {
		req := &RequestAccountUpdates{}
		req.Subscribe = false
		req.AccountCode = p.unsubscribe
		if err := eng.Send(req); err != nil {
			return true, err
		}
	}

	next := ""
	replace := []string{}
	for _, acct := range p.accountCode {
		if next == "" {
			next = acct
		} else {
			replace = append(replace, acct)
		}
	}
	p.accountCode = replace
	p.unsubscribe = next

	if next == "" {
		return true, nil
	}

	req := &RequestAccountUpdates{}
	req.Subscribe = true
	req.AccountCode = next
	if err := eng.Send(req); err != nil {
		return true, err
	}

	return false, nil
}

func (p *PrimaryAccountManager) preDestroy() {
	eng := p.engine()
	rc := p.replyCh()

	eng.Unsubscribe(rc, p.id)
	req := &RequestAccountUpdates{}
	req.Subscribe = false
	req.AccountCode = p.unsubscribe
	eng.Send(req)
}

// Values returns the most recent snapshot of account information.
func (p *PrimaryAccountManager) Values() map[AccountValueKey]AccountValue {
	am := p.am()
	am.RLock()
	defer am.RUnlock()
	return p.values
}

// Portfolio returns the most recent snapshot of account portfolio.
func (p *PrimaryAccountManager) Portfolio() map[PortfolioValueKey]PortfolioValue {
	am := p.am()
	am.RLock()
	defer am.RUnlock()
	return p.portfolio
}
