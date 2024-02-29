## Template to deploy the GMSA webhook
## TODO: make this a helmchart instead?

apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  labels:
    gmsa-webhook: disabled

---

apiVersion: v1
kind: Secret
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}
data:
  tls_private_key: ${TLS_PRIVATE_KEY}
  tls_certificate: ${TLS_CERTIFICATE}

---

# the service account for the webhook
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}

---

# the RBAC role that the webhook needs to:
#  * read GMSA custom resources
#  * check authorizations to use GMSA cred specs
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: ${RBAC_ROLE_NAME}
rules:
- apiGroups: ["windows.k8s.io"]
  resources: ["gmsacredentialspecs"]
  verbs: ["get"]
- apiGroups: ["authorization.k8s.io"]
  resources: ["localsubjectaccessreviews"]
  verbs: ["create"]

---

# bind that role to the webhook's service account
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: ${NAMESPACE}-${NAME}-binding-to-${RBAC_ROLE_NAME}
  namespace: ${NAMESPACE}
subjects:
- kind: ServiceAccount
  name: ${NAME}
  namespace: ${NAMESPACE}
roleRef:
  kind: ClusterRole
  name: ${RBAC_ROLE_NAME}
  apiGroup: rbac.authorization.k8s.io

---

apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${NAME}
  template:
    metadata:
      labels:
        app: ${NAME}
    spec:
      serviceAccountName: ${NAME}
      nodeSelector:
        kubernetes.io/os: linux${TOLERATIONS}
      containers:
      - name: ${NAME}
        image: ${IMAGE_NAME}
        imagePullPolicy: IfNotPresent
        readinessProbe:
          httpGet:
            scheme: HTTPS
            path: /health
            port: 443
        ports:
        - containerPort: 443
        volumeMounts:
          - name: tls
            mountPath: "/tls"
            readOnly: true
        env:
          - name: TLS_KEY
            value: /tls/key
          - name: TLS_CRT
            value: /tls/crt
      volumes:
      - name: tls
        secret:
          secretName: ${NAME}
          items:
          - key: tls_private_key
            path: key
          - key: tls_certificate
            path: crt

---

apiVersion: v1
kind: Service
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}
spec:
  ports:
  - port: 443
    targetPort: 443
  selector:
    app: ${NAME}

---

apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: ${NAME}
webhooks:
- name: admission-webhook.windows-gmsa.sigs.k8s.io
  clientConfig:
    service:
      name: ${NAME}
      namespace: ${NAMESPACE}
      path: "/validate"
    caBundle: ${CA_BUNDLE}
  rules:
  - operations: ["CREATE", "UPDATE"]
    apiGroups: [""]
    apiVersions: ["*"]
    resources: ["pods"]
  failurePolicy: Fail
  admissionReviewVersions: ["v1", "v1beta1"]
  sideEffects: None
  # don't run on ${NAMESPACE}
  namespaceSelector:
    matchExpressions:
      - key: gmsa-webhook
        operator: NotIn
        values: [disabled]

---

apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ${NAME}
webhooks:
- name: admission-webhook.windows-gmsa.sigs.k8s.io
  clientConfig:
    service:
      name: ${NAME}
      namespace: ${NAMESPACE}
      path: "/mutate"
    caBundle: ${CA_BUNDLE}
  rules:
  - operations: ["CREATE"]
    apiGroups: [""]
    apiVersions: ["*"]
    resources: ["pods"]
  failurePolicy: Fail
  admissionReviewVersions: ["v1", "v1beta1"]
  sideEffects: None
  # don't run on ${NAMESPACE}
  namespaceSelector:
    matchExpressions:
    - key: gmsa-webhook
      operator: NotIn
      values: [disabled]
