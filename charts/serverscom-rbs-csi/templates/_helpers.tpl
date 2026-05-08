{{/*
Validate StorageClass fsType against what the driver supports for both
format and online/offline resize.
Empty value is allowed (driver defaults to ext4).
*/}}
{{- define "rbs-csi.validateFsType" -}}
{{- $fs := . -}}
{{- if $fs -}}
    {{- $allowed := list "ext4" "ext3" "xfs" -}}
    {{- if not (has $fs $allowed) -}}
        {{- fail (printf "storageClasses.fsType %q is not supported (allowed: %s). The driver can format other types but cannot resize them, which conflicts with allowVolumeExpansion: true." $fs (join ", " $allowed)) -}}
    {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Common labels merged onto every managed object.
*/}}
{{- define "rbs-csi.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/name: {{ .Chart.Name | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
{{- range $k, $v := .Values.commonLabels }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}
