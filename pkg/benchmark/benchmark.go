package benchmark

import (
	"fmt"
	"imageprocessor/pkg/ocr"
	"imageprocessor/pkg/pdf"
	"imageprocessor/pkg/translator"
	"log"
	"runtime"
	"time"
)

// BenchmarkResult represents the result of a benchmark run
type BenchmarkResult struct {
	OCRTime         time.Duration
	TranslationTime time.Duration
	PDFTime         time.Duration
	TotalTime       time.Duration
	CacheHits       int
	CacheMisses     int
}

// CPUInfo holds information about the CPU
type CPUInfo struct {
	Cores       int
	Threads     int
	UseParallel bool
}

// PerformanceSummary represents a summary of performance metrics
type PerformanceSummary struct {
	DirectExecution    BenchmarkResult
	CachedExecution    BenchmarkResult
	QueuedExecution    BenchmarkResult
	ImprovementPercent float64
	CPUInfo            CPUInfo
}

// GetCPUInfo returns information about the CPU
func GetCPUInfo() CPUInfo {
	return CPUInfo{
		Cores:       runtime.NumCPU(),
		Threads:     runtime.GOMAXPROCS(0),
		UseParallel: runtime.GOMAXPROCS(0) > 1,
	}
}

// RunDirectBenchmark runs a benchmark of direct processing without cache
func RunDirectBenchmark(imagePath string) BenchmarkResult {
	var result BenchmarkResult

	// Clear caches
	ocr.ClearCache()
	translator.ClearCache()

	// Measure OCR time
	startTime := time.Now()
	text, err := ocr.ImageToText(imagePath)
	if err != nil {
		log.Printf("OCR error: %v", err)
		return result
	}
	result.OCRTime = time.Since(startTime)

	// Measure translation time
	startTime = time.Now()
	translatedText, err := translator.Translate(text)
	if err != nil {
		log.Printf("Translation error: %v", err)
		return result
	}
	result.TranslationTime = time.Since(startTime)

	// Measure PDF generation time
	startTime = time.Now()
	_, err = pdf.CreatePDF(translatedText)
	if err != nil {
		log.Printf("PDF creation error: %v", err)
		return result
	}
	result.PDFTime = time.Since(startTime)

	// Calculate total time
	result.TotalTime = result.OCRTime + result.TranslationTime + result.PDFTime

	return result
}

// RunCachedBenchmark runs a benchmark of direct processing with cache
func RunCachedBenchmark(imagePath string) BenchmarkResult {
	// First run to populate cache
	RunDirectBenchmark(imagePath)

	// Now benchmark with cache
	var result BenchmarkResult

	// Measure OCR time with cache
	startTime := time.Now()
	text, err := ocr.ImageToText(imagePath)
	if err != nil {
		log.Printf("OCR error: %v", err)
		return result
	}
	result.OCRTime = time.Since(startTime)

	// Measure translation time with cache
	startTime = time.Now()
	translatedText, err := translator.Translate(text)
	if err != nil {
		log.Printf("Translation error: %v", err)
		return result
	}
	result.TranslationTime = time.Since(startTime)

	// Measure PDF generation time
	startTime = time.Now()
	_, err = pdf.CreatePDF(translatedText)
	if err != nil {
		log.Printf("PDF creation error: %v", err)
		return result
	}
	result.PDFTime = time.Since(startTime)

	// Calculate total time
	result.TotalTime = result.OCRTime + result.TranslationTime + result.PDFTime

	return result
}

// FormatBenchmarkResult formats a benchmark result for display
func FormatBenchmarkResult(result BenchmarkResult) string {
	return fmt.Sprintf(
		"OCR: %v\nTranslation: %v\nPDF Generation: %v\nTotal: %v",
		result.OCRTime,
		result.TranslationTime,
		result.PDFTime,
		result.TotalTime,
	)
}

// CalculateImprovement calculates the percentage improvement between two benchmark results
func CalculateImprovement(baseline, improved BenchmarkResult) float64 {
	if baseline.TotalTime == 0 {
		return 0
	}
	return 100 * (1 - float64(improved.TotalTime)/float64(baseline.TotalTime))
}

// GeneratePerformanceSummary generates a human-readable performance summary
func GeneratePerformanceSummary(direct, cached BenchmarkResult) string {
	cpuInfo := GetCPUInfo()
	improvement := CalculateImprovement(direct, cached)

	return fmt.Sprintf(`
Performance Summary
==================

Hardware Information:
- CPU Cores: %d
- Threads: %d
- Parallel Execution: %t

Direct Execution (No Cache):
%s

Cached Execution:
%s

Performance Improvement: %.2f%%

Execution Time Breakdown:
- OCR Processing: %.2f%% of total time
- Translation: %.2f%% of total time
- PDF Generation: %.2f%% of total time

Cache Impact:
- OCR Speedup: %.2fx
- Translation Speedup: %.2fx
- Overall Speedup: %.2fx

Recommendation:
%s
`,
		cpuInfo.Cores,
		cpuInfo.Threads,
		cpuInfo.UseParallel,
		FormatBenchmarkResult(direct),
		FormatBenchmarkResult(cached),
		improvement,
		float64(direct.OCRTime)/float64(direct.TotalTime)*100,
		float64(direct.TranslationTime)/float64(direct.TotalTime)*100,
		float64(direct.PDFTime)/float64(direct.TotalTime)*100,
		float64(direct.OCRTime)/float64(cached.OCRTime),
		float64(direct.TranslationTime)/float64(cached.TranslationTime),
		float64(direct.TotalTime)/float64(cached.TotalTime),
		generateRecommendation(direct, cached, cpuInfo),
	)
}

// generateRecommendation generates performance recommendations based on benchmark results
func generateRecommendation(direct, cached BenchmarkResult, cpuInfo CPUInfo) string {
	var recommendations []string

	// Check if OCR is the bottleneck
	if float64(direct.OCRTime)/float64(direct.TotalTime) > 0.5 {
		recommendations = append(recommendations,
			"- OCR is the primary bottleneck. Consider increasing the OCR filter optimization.",
			"- Increase OCR worker count to match available CPU cores.")
	}

	// Check if caching provides significant benefits
	if float64(direct.TotalTime)/float64(cached.TotalTime) > 2.0 {
		recommendations = append(recommendations,
			"- Caching provides significant performance benefits. Consider increasing cache TTL.",
			"- Implement distributed cache like Redis for better scalability.")
	} else {
		recommendations = append(recommendations,
			"- Caching benefits are moderate. Focus on optimizing the processing pipeline.")
	}

	// Check CPU utilization
	if cpuInfo.Cores > 2 {
		recommendations = append(recommendations,
			"- Your system has multiple cores. Message queue with parallel workers is recommended.",
			fmt.Sprintf("- Optimal worker configuration: %d OCR workers, %d translation workers, %d PDF workers.",
				cpuInfo.Cores/2, cpuInfo.Cores/4, cpuInfo.Cores/4))
	}

	if len(recommendations) == 0 {
		return "No specific recommendations available."
	}

	result := ""
	for _, rec := range recommendations {
		result += rec + "\n"
	}
	return result
}
