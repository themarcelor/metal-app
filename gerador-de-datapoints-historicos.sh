#!/bin/bash

cat << EOF
# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
EOF

# Data/Hora atual
AGORA=$(date +"%s")
# Numero de datapoints historicos
NUMERO_DE_MINUTOS=60
# As metricas vao comecar a surgir aqui
PONTO_DE_PARTIDA=$(($AGORA - ($NUMERO_DE_MINUTOS * 60)))

NUMERO_DE_ERROS=0
MINUTO_EM_QUE_ERROS_COMECAM_A_OCORRER=10

for i in $( seq 1 $NUMERO_DE_MINUTOS )
do
   PROXIMA_DATA=$(($PONTO_DE_PARTIDA + ($i * 60)))
   echo "http_requests_total{code=\"200\",service=\"servicoX\"} 1000 $PROXIMA_DATA"
   echo "http_requests_total{code=\"500\",service=\"servicoX\"} $NUMERO_DE_ERROS $PROXIMA_DATA"
   if [[ $i -ge $MINUTO_EM_QUE_ERROS_COMECAM_A_OCORRER ]]; then
     # Entramos na janela de erros, vamos incrementar o num de erros a cada minuto
     NUMERO_DE_ERROS=$((NUMERO_DE_ERROS+1))
   fi
done

echo "# EOF"
