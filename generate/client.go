package generate

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudflare/cfssl/log"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Client struct {
	client kubernetes.Interface
}

func (c *Client) ClientSet() kubernetes.Interface {
	return c.client
}

func NewClient(client kubernetes.Interface) *Client {
	return &Client{client: client}
}

func (kt *Client) createNsIfNotExist(namespace string) error {
	_, err := kt.client.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}

		if _, err := kt.client.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{}); err != nil {
			return err
		}

	}

	return nil
}

func (kt *Client) reCreateRoleBinding(roleBindingType, name, username, namespace, roleRef, saNameSpace string) error {
	_, err := kt.client.RbacV1().RoleBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err == nil {
		if err := kt.client.RbacV1().RoleBindings(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	var subj []rbacv1.Subject
	if roleBindingType == "User" {
		subj = []rbacv1.Subject{
			{
				Kind: rbacv1.UserKind,
				Name: username,
			},
		}
	} else {
		subj = []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      username,
				Namespace: saNameSpace,
			},
		}
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subjects: subj,
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: roleRef,
		},
	}

	if _, err := kt.client.RbacV1().RoleBindings(namespace).Create(context.TODO(), rb, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

func (kt *Client) reCreateClusterRoleBinding(roleBindingType, name, username, roleRef, saNameSpace string) error {
	_, err := kt.client.RbacV1().ClusterRoleBindings().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err == nil {
		if err := kt.client.RbacV1().ClusterRoleBindings().Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	var subj []rbacv1.Subject
	if roleBindingType == "User" {
		subj = []rbacv1.Subject{
			{
				Kind: rbacv1.UserKind,
				Name: username,
			},
		}
	} else {
		subj = []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      username,
				Namespace: saNameSpace,
			},
		}
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: subj,
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: roleRef,
		},
	}

	if _, err := kt.client.RbacV1().ClusterRoleBindings().Create(context.TODO(), crb, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

func (kt *Client) ReCreateK8sCSR(cn, csrStr string) error {
	k8sCSR := certv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: cn,
		},
		Spec: certv1.CertificateSigningRequestSpec{
			Request: []byte(csrStr),
			Usages: []certv1.KeyUsage{
				certv1.UsageClientAuth,
			},
			SignerName: certv1.KubeAPIServerClientSignerName,
		},
	}
	err := kt.client.CertificatesV1().CertificateSigningRequests().Delete(context.TODO(), k8sCSR.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if _, err := kt.client.CertificatesV1().CertificateSigningRequests().Create(context.TODO(), &k8sCSR, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

func (kt *Client) WaitForK8sCsrReady(name string) (csr *certv1.CertificateSigningRequest, err error) {
	for i := 0; i < 5; i++ {
		csr, err = kt.client.CertificatesV1().CertificateSigningRequests().Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			log.Errorf("get %s csr err: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if len(csr.Status.Certificate) == 0 {
			time.Sleep(time.Second)
			continue
		}

		for _, c := range csr.Status.Conditions {
			if c.Type == certv1.CertificateApproved {
				return
			}
		}
	}
	return nil, fmt.Errorf("wait csr to be approved timeout")
}

func (kt *Client) ApprovalK8sCSR(name string) error {
	k8sCSR := &certv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: certv1.CertificateSigningRequestStatus{
			Conditions: []certv1.CertificateSigningRequestCondition{
				{Type: certv1.CertificateApproved, Status: corev1.ConditionTrue, LastUpdateTime: metav1.Now(), Message: "approval", Reason: "approval"},
			},
		},
	}

	if _, err := kt.client.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), name, k8sCSR, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

func (kt *Client) GetServiceAccountNames(nameSpace string) []string {
	if err := kt.createNsIfNotExist(nameSpace); err != nil {
		log.Fatalf("create ns: %v err: %v", nameSpace, err)
	}

	serviceAccounts, err := kt.client.CoreV1().ServiceAccounts(nameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("list cluster role err: %v", err)
	}
	var accountNames []string
	for _, item := range serviceAccounts.Items {
		accountNames = append(accountNames, item.Name)
	}
	return accountNames
}

func (kt *Client) GetClusterRoleNames() []string {
	clusterRoles, err := kt.client.RbacV1().ClusterRoles().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var clusterRoleNames []string

	for _, item := range clusterRoles.Items {
		clusterRoleNames = append(clusterRoleNames, item.Name)
	}
	return clusterRoleNames
}

func (kt *Client) GenerateBinding(roleBindingType, saNameSpace, username string, clusterRoles []string, namespaces []string) error {
	if len(namespaces) == 0 {
		for _, cr := range clusterRoles {
			name := fmt.Sprintf("%s-%s", username, cr)
			if err := kt.reCreateClusterRoleBinding(roleBindingType, name, username, cr, saNameSpace); err != nil {
				return fmt.Errorf("create cluster role binding for %s err: %w", username, err)
			}
			log.Infof("create cluster role binding %s success", name)
		}
	} else {
		for _, ns := range namespaces {
			if err := kt.createNsIfNotExist(ns); err != nil {
				return fmt.Errorf("create namespace %s,err: %w", ns, err)
			}

			for _, cr := range clusterRoles {
				name := fmt.Sprintf("%s-%s", username, cr)
				if err := kt.reCreateRoleBinding(roleBindingType, name, username, ns, cr, saNameSpace); err != nil {
					return fmt.Errorf("create role binding for %s err: %w", username, err)
				}
				log.Infof("create role binding %s in %s namespace success", name, ns)
			}
		}
	}
	return nil
}
