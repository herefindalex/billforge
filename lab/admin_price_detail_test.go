package lab

import (
	"context"
	"fmt"
	"testing"
)

func TestAdminPriceVersionDetailBoundsMalformedDraftComponents(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state) VALUES('draft-many-components','draft-plan',1,'USD',100,0,'','draft')`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 51; index++ {
		code := fmt.Sprintf("component-%02d", index)
		if _, err := l.db.ExecContext(ctx, `INSERT INTO price_components(price_version_id,component_code,kind,amount_minor) VALUES('draft-many-components',?,'per_seat',1)`, code); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := l.AdminPriceVersionDetail(ctx, "draft-many-components")
	if err != nil {
		t.Fatal(err)
	}
	lastCode := ""
	if len(detail.Components) > 0 {
		lastCode = detail.Components[len(detail.Components)-1].Code
	}
	if detail.PublicationState != "draft" || len(detail.Components) != 50 || !detail.ComponentsTruncated || lastCode != "component-49" {
		t.Fatalf("unbounded draft detail: state=%s components=%d truncated=%t last=%s", detail.PublicationState, len(detail.Components), detail.ComponentsTruncated, lastCode)
	}
}
