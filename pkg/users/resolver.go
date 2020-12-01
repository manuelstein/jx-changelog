package users

import (
	"context"
	"fmt"
	"strings"

	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube/naming"
	"github.com/jenkins-x/jx-helpers/v3/pkg/scmhelpers"
	"github.com/pkg/errors"

	"gopkg.in/src-d/go-git.v4/plumbing/object"

	jenkinsv1 "github.com/jenkins-x/jx-api/v3/pkg/apis/jenkins.io/v1"
)

// GitUserResolver allows git users to be converted to Jenkins X users
type GitUserResolver struct {
	GitProvider *scm.Client
	cache       UserDetailService
}

// GitSignatureAsUser resolves the signature to a Jenkins X User
func (r *GitUserResolver) GitSignatureAsUser(signature *object.Signature) (*jenkinsv1.UserDetails, error) {
	// We can't resolve no info so shortcircuit
	if signature.Name == "" && signature.Email == "" {
		return nil, nil
	}
	gitUser := &scm.User{
		Email: signature.Email,
		Name:  signature.Name,
	}
	return r.Resolve(gitUser)
}

// GitUserSliceAsUserDetailsSlice resolves a slice of git users to a slice of Jenkins X User Details
func (r *GitUserResolver) GitUserSliceAsUserDetailsSlice(users []scm.User) ([]jenkinsv1.UserDetails, error) {
	var answer []jenkinsv1.UserDetails
	for _, user := range users {
		us := user
		u, err := r.Resolve(&us)
		if err != nil {
			return nil, err
		}
		if u != nil {
			answer = append(answer, *u)
		}
	}
	return answer, nil
}

// Resolve will convert the GitUser to a Jenkins X user and attempt to complete the user info by:
// * checking the user custom resources to see if the user is present there
// * making a call to the gitProvider
// as often user info is not complete in a git response
func (r *GitUserResolver) Resolve(user *scm.User) (*jenkinsv1.UserDetails, error) {
	if r == nil || user == nil || user.Name == "" {
		return nil, nil
	}

	u := r.cache.GetUser(user.Name)
	if u != nil {
		return u, nil
	}

	ctx := context.Background()

	if user.Login == "" {
		u = r.GitUserToUser(user)
		err := r.cache.CreateOrUpdateUser(u)
		if err != nil {
			return u, errors.Wrapf(err, "failed to cache User")
		}
		return u, nil
	}

	scmUser, _, err := r.GitProvider.Users.FindLogin(ctx, user.Login)
	if scmUser == nil || scmhelpers.IsScmNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find user %s", user.Login)
	}

	u = r.GitUserToUser(scmUser)
	login := scmUser.Login
	if login == "" {
		login = strings.Replace(scmUser.Name, " ", "-", -1)
		login = strings.ToLower(login)
	}
	id := naming.ToValidName(login)
	u.Name = naming.ToValidName(id)
	err = r.cache.CreateOrUpdateUser(u)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create User")
	}
	return u, nil
}

/* TODO
// UpdateUserFromPRAuthor will attempt to use the
func (r *GitUserResolver) UpdateUserFromPRAuthor(author *jenkinsv1.User, pullRequest *scm.PullRequest,
	commits []*gits.GitCommit) (*jenkinsv1.User, error) {

	if pullRequest != nil {
		updated := false
		if author != nil {
			gitLogin := r.GitUserLogin(author)
			if gitLogin == "" {
				gitLogin = author.Spec.Login
			}
			for _, commit := range commits {
				if commit.Author != nil && gitLogin == commit.Author.Login {
					log.Logger().Info("Found commit author match for: " + author.
						Spec.Login + " with email address: " + commit.Author.Email + "\n")
					author.Spec.Email = commit.Author.Email
					updated = true
					break
				}
			}
		}
		if updated {
			return r.JXClient.JenkinsV1().Users(r.Namespace).PatchUpdate(author)
		}
	}
	return author, nil
}
*/

// UserToGitUser performs type conversion from a Jenkins X User to a Git User
func (r *GitUserResolver) UserToGitUser(id string, user *jenkinsv1.User) *scm.User {
	return &scm.User{
		Login:  id,
		Email:  user.Spec.Email,
		Name:   user.Spec.Name,
		Link:   user.Spec.URL,
		Avatar: user.Spec.AvatarURL,
	}
}

// GitUserToUser performs type conversion from a GitUser to a Jenkins X user,
// attaching the Git Provider account to Accounts
func (r *GitUserResolver) GitUserToUser(gitUser *scm.User) *jenkinsv1.UserDetails {
	return &jenkinsv1.UserDetails{
		Login: gitUser.Login,
		Name:  gitUser.Name,
		Email: gitUser.Email,
	}
}

// GitUserLogin returns the login for the git provider, or an empty string if not found
func (r *GitUserResolver) GitUserLogin(user *jenkinsv1.User) string {
	for _, a := range user.Spec.Accounts {
		if a.Provider == r.GitProviderKey() {
			return a.ID
		}
	}
	return ""
}

// GitProviderKey returns the provider key for this GitUserResolver
func (r *GitUserResolver) GitProviderKey() string {
	if r == nil || r.GitProvider == nil {
		return ""
	}
	return fmt.Sprintf("jenkins.io/git-%s-userid", r.GitProvider.Driver.String())
}

// mergeGitUsers merges user1 into user2, replacing any that do not have empty values on user2 with those from user1
