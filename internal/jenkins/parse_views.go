package jenkins

import "github.com/Breina/Jenking/internal/domain/jmodel"

// Pure parser for the Jenkins views JSON, kept adapter-side and independently
// testable (architecture.md §6).

// parseViewList converts a decoded view container into domain views, marking
// the container's primary view.
func parseViewList(resp jsonViewContainer, owner string, personal bool) []jmodel.JenkinsView {
	views := make([]jmodel.JenkinsView, 0, len(resp.Views))
	for _, v := range resp.Views {
		if v.Name == "" {
			continue
		}
		views = append(views, jmodel.JenkinsView{
			Name:      v.Name,
			URL:       v.URL,
			Kind:      jmodel.ParseViewKind(v.Class),
			OwnerPath: owner,
			Personal:  personal,
			IsPrimary: resp.PrimaryView != nil && resp.PrimaryView.Name == v.Name,
		})
	}
	return views
}
