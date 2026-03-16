package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	//"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	instrument "go.opentelemetry.io/otel/metric"
	otel_metric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var meuContador otel_metric.Int64Counter

var tracer trace.Tracer

type IgnoreCaminhoSampler struct{}

func (ics *IgnoreCaminhoSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	result := sdktrace.SamplingResult{
		Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
	}
	shouldSample := true
	fmt.Println("### SHOULD SAMPLE EXECUTANDO!!!")
	for _, att := range p.Attributes {
		if att.Key == "http.target" && att.Value.AsString() == "/meh" {
			shouldSample = false
			break
		}
	}
	if shouldSample {
		result.Decision = sdktrace.RecordAndSample
	} else {
		result.Decision = sdktrace.Drop
	}
	return result
}

func (ics IgnoreCaminhoSampler) Description() string {
	return "excludeBasedOnURLPath"
}

func logFilePath() string {
	if p := os.Getenv("LOG_FILE_PATH"); p != "" {
		return p
	}
	return "minhaApp.log"
}

var (
	outfile, _ = os.OpenFile(logFilePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	logger     = log.New(os.Stdout, "", 0)
)

func writeJSONLog(level, message string) {
	entry := map[string]string{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"level":     level,
		"message":   message,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		logger.Printf("ERROR marshalling log entry: %v", err)
		return
	}
	fmt.Fprintln(outfile, string(b))
}

func main() {
	ctx := context.Background()

	serviceName := "metal-app"
	collectorAddress := os.Getenv("OTEL_COLLECTOR_ADDRESS")
	if collectorAddress == "" {
		collectorAddress = "localhost:4317"
	}
	logger.Printf("Establishing gRPC connection with %s...\n", collectorAddress)

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5 * time.Second),
	}
	conn, err := grpc.DialContext(ctx, collectorAddress, dopts...)
	if err != nil {
		log.Fatalf("ERROR: Unable to establish grpc connection to otel metrics collector: %w", err)
	}

	exporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		log.Fatalf("unable to create otel grpc exporter: %w", err)
	}

	var attrs []attribute.KeyValue
	// Always include service_name by default
	attrs = append(attrs, semconv.ServiceName(serviceName))

	res, err := resource.New(ctx, resource.WithAttributes(
		attrs...,
	))
	if err != nil {
		log.Fatalf("failed to create resource: %w", err)
	}

	// metrics
	provider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter)),
	)
	meter := provider.Meter("sre")

	c, err := meter.Int64Counter("sre.meu.contador")
	if err != nil {
		log.Fatalf("unable to create counter metric", err)
	}
	meuContador = c

	// traces
	collectorTracesAddress := os.Getenv("OTEL_TRACES_ADDRESS")
	if collectorTracesAddress == "" {
		collectorTracesAddress = "localhost:4417"
	}
	t, _ := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(collectorTracesAddress), otlptracegrpc.WithInsecure())
	//exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	//if err != nil {
	//	log.Fatalf("failed to initialize stdouttrace exporter: %w", err)

	//}
	//bsp := sdktrace.NewBatchSpanProcessor(exp)
	bsp := sdktrace.NewBatchSpanProcessor(t)
	ignoreCaminhoSampler := new(IgnoreCaminhoSampler)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(ignoreCaminhoSampler),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tp)
	tracer = otel.Tracer(serviceName)

	mux := http.NewServeMux()
	mux.Handle("/", otelhttp.NewHandler(otelhttp.WithRouteTag("/", http.HandlerFunc(HelloServer)), "root", otelhttp.WithPublicEndpoint()))

	http.HandleFunc("/", HelloServer)
	http.ListenAndServe(":8080", mux)
}

func HelloServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var attrs = []attribute.KeyValue{
		attribute.Key("operacao").String("oi"),
	}
	opt := instrument.WithAttributes(attrs...)

	writeJSONLog("INFO", "emitindo metrica OTel...")
	meuContador.Add(ctx, 1, opt)

	if r.URL.Path[1:] == "erro" {
		writeJSONLog("ERROR", "request falhou: caminho /erro acionado")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("deu errado"))
		return
	}

	writeJSONLog("DEBUG", fmt.Sprintf("processando caminho: %s", r.URL.Path[1:]))
	fmt.Fprintf(w, OtherFunction(ctx, r.URL.Path[1:]))
}

func OtherFunction(ctx context.Context, nome string) string {
	_, span := tracer.Start(
		ctx,
		"digaOla",
		trace.WithAttributes(attribute.String("AlgumAtributo", "QualquerValor")),
	)
	if span != nil {
		defer span.End()
	}

	return fmt.Sprintf("Olá, %s!", nome)
}
