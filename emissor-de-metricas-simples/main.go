package main

import (
	"context"
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
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	//"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	instrument "go.opentelemetry.io/otel/metric"
	otel_metric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var meuContador otel_metric.Int64Counter

var (
	outfile, _ = os.Create("minhaApp.log")
	logger     = log.New(outfile, "", 0)
)

func main() {
	ctx := context.Background()

	serviceName := "metal-app"
	collectorAddress := "localhost:4317"
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
	//	jaegerEndpoint := "localhost:14250"
	//	jg, _ := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(jaegerEndpoint), otlptracegrpc.WithInsecure())
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatalf("failed to initialize stdouttrace exporter: %w", err)
	}
	bsp := sdktrace.NewBatchSpanProcessor(exp)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(bsp),
		//sdktrace.WithBatcher(jg),
	)
	otel.SetTracerProvider(tp)

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

	logger.Print("emitindo metrica OTel...")
	meuContador.Add(ctx, 1, opt)

	fmt.Fprintf(w, "Olá, %s!", r.URL.Path[1:])
}
