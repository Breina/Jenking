package view

import (
	"testing"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

func leaf(k, v string) jmodel.MetaNode { return jmodel.MetaNode{Key: k, Value: v} }
func cont(k string, kids ...jmodel.MetaNode) jmodel.MetaNode {
	return jmodel.MetaNode{Key: k, Container: true, Children: kids}
}

func TestFindObjectURL_PrefersTopLevelAction(t *testing.T) {
	root := cont("",
		leaf("_class", "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"),
		cont("actions",
			cont("[0]"), // unrelated empty action
			cont("[1]",
				leaf("_class", objectMetadataClass),
				leaf("objectDisplayName", "migratie / jenkins-e2e"),
				leaf("objectUrl", "https://git.example/migratie/jenkins-e2e"),
			),
		),
		cont("jobs",
			cont("[0]",
				cont("actions",
					cont("[0]",
						leaf("_class", objectMetadataClass),
						leaf("objectUrl", "https://git.example/migratie/jenkins-e2e/-/tree/main"),
					),
				),
			),
		),
	)

	got := findObjectURL(root)
	if want := "https://git.example/migratie/jenkins-e2e"; got != want {
		t.Errorf("findObjectURL = %q, want top-level repo URL %q", got, want)
	}
}

func TestFindObjectURL_NoneWhenAbsent(t *testing.T) {
	root := cont("",
		leaf("_class", "hudson.model.FreeStyleProject"),
		cont("actions", cont("[0]", leaf("_class", "hudson.model.CauseAction"))),
	)
	if got := findObjectURL(root); got != "" {
		t.Errorf("findObjectURL = %q, want empty", got)
	}
}
