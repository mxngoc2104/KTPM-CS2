import { useState, useEffect, useCallback } from 'react';
import type { ChangeEvent } from 'react';
import axios, { AxiosError } from 'axios';
import './App.css'; // Bạn có thể thêm CSS để giao diện đẹp hơn

// Cấu hình URL của API backend
const API_BASE_URL = 'http://localhost:8080/api'; // Chạy API Go trên cổng 8080

// Định nghĩa kiểu dữ liệu cho trạng thái Job (có thể mở rộng)
type JobStatus = 'queued' | 'processing' | 'completed' | 'failed' | 'upload_failed' | 'status_error' | 'Uploading...' | null;

// Định nghĩa kiểu dữ liệu cho response từ API status
interface StatusResponse {
  job_id: string;
  status: JobStatus;
  pdf_path?: string;
  error_message?: string;
  cached?: boolean; // Thêm trạng thái cache
  filter_ms?: string; // Thời gian dạng string (từ Redis)
  ocr_ms?: string;
  translate_ms?: string;
  pdf_ms?: string;
}

// Định nghĩa kiểu dữ liệu cho response từ API upload
interface UploadResponse {
    job_id: string;
    message: string;
}

// Định nghĩa kiểu dữ liệu cho lỗi API (ví dụ)
interface ApiError {
    error: string;
}

function App() {
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [jobStatus, setJobStatus] = useState<JobStatus>(null);
  const [errorMessage, setErrorMessage] = useState<string>('');
  const [pdfPath, setPdfPath] = useState<string | null>(null); // Lưu pdfPath từ status
  const [isUploading, setIsUploading] = useState<boolean>(false);
  const [isCheckingStatus, setIsCheckingStatus] = useState<boolean>(false);
  const [pollingIntervalId, setPollingIntervalId] = useState<number | null>(null);
  // --- Thêm state cho thông tin chi tiết ---
  const [isCached, setIsCached] = useState<boolean | null>(null);
  const [filterMs, setFilterMs] = useState<string | null>(null);
  const [ocrMs, setOcrMs] = useState<string | null>(null);
  const [translateMs, setTranslateMs] = useState<string | null>(null);
  const [pdfMs, setPdfMs] = useState<string | null>(null);
  // ----------------------------------------

  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (event.target.files && event.target.files[0]) {
      setSelectedFile(event.target.files[0]);
      // Reset trạng thái (bao gồm cả thông tin chi tiết)
      setJobId(null);
      setJobStatus(null);
      setErrorMessage('');
      setPdfPath(null);
      setIsCached(null);
      setFilterMs(null);
      setOcrMs(null);
      setTranslateMs(null);
      setPdfMs(null);
      stopPolling();
    }
  };

  const handleUpload = async () => {
    if (!selectedFile) {
      setErrorMessage('Please select an image file first.');
      return;
    }

    const formData = new FormData();
    formData.append('image', selectedFile); // Tên field 'image' phải khớp với backend Gin

    setIsUploading(true);
    setErrorMessage('');
    setJobStatus('Uploading...');

    try {
      const response = await axios.post<UploadResponse>(`${API_BASE_URL}/upload`, formData, {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
      });
      const newJobId = response.data.job_id;
      setJobId(newJobId);
      setJobStatus('queued'); // Trạng thái ban đầu sau khi upload thành công
      console.log('Upload successful, Job ID:', newJobId);
      startPolling(newJobId); // Bắt đầu kiểm tra status tự động
    } catch (error) {
      console.error('Upload failed:', error);
      let errorMsg = 'Upload failed. Please check the console.';
      if (axios.isAxiosError(error)) { // Kiểm tra nếu là AxiosError
           const axiosError = error as AxiosError<ApiError>; // Ép kiểu sang AxiosError với kiểu dữ liệu lỗi dự kiến
           if(axiosError.response?.data?.error){
               errorMsg = axiosError.response.data.error;
           } else {
               errorMsg = axiosError.message;
           }
      } else if (error instanceof Error) {
           errorMsg = error.message;
      }
      setErrorMessage(errorMsg);
      setJobStatus('upload_failed');
    } finally {
      setIsUploading(false);
    }
  };

  // useCallback để tránh tạo lại hàm checkStatus mỗi lần render trừ khi pollingIntervalId thay đổi
  const checkStatus = useCallback(async (currentJobId: string | null) => {
    if (!currentJobId) return;

    setIsCheckingStatus(true);
    console.log(`Checking status for job: ${currentJobId}`);
    try {
      const response = await axios.get<StatusResponse>(`${API_BASE_URL}/status/${currentJobId}`);
      // Lấy các thông tin mới từ response
      const { status, pdf_path, error_message, cached, filter_ms, ocr_ms, translate_ms, pdf_ms } = response.data;
      console.log('Status response:', response.data);
      setJobStatus(status);
      if (status === 'completed') {
        setPdfPath(pdf_path ?? null);
        setErrorMessage('');
        // Cập nhật state chi tiết
        setIsCached(cached ?? null);
        setFilterMs(filter_ms ?? null);
        setOcrMs(ocr_ms ?? null);
        setTranslateMs(translate_ms ?? null);
        setPdfMs(pdf_ms ?? null);
        stopPolling();
      } else if (status === 'failed') {
        setErrorMessage(error_message || 'Processing failed.');
        setPdfPath(null);
        // Reset thông tin chi tiết khi lỗi
        setIsCached(null);
        setFilterMs(null);
        setOcrMs(null);
        setTranslateMs(null);
        setPdfMs(null);
        stopPolling();
      } else {
        setPdfPath(null);
        setErrorMessage('');
        // Reset thông tin chi tiết khi đang xử lý
        setIsCached(null);
        setFilterMs(null);
        setOcrMs(null);
        setTranslateMs(null);
        setPdfMs(null);
      }
    } catch (error) {
      console.error('Failed to check status:', error);
      let errorMsg = 'Failed to check status.';
      let shouldStopPolling = false;
      let statusOnError: JobStatus = null;

      if (axios.isAxiosError(error)) {
           const axiosError = error as AxiosError<ApiError>;
           if(axiosError.response?.data?.error){
               errorMsg = axiosError.response.data.error;
           } else {
               errorMsg = axiosError.message;
           }
           if (axiosError.response?.status === 404) {
               errorMsg = `Job ${currentJobId} not found.`;
               shouldStopPolling = true; // Dừng polling nếu job không tồn tại
               statusOnError = 'status_error';
           } else if (axiosError.response?.status && axiosError.response.status >= 500) {
               errorMsg = `Server error checking status for job ${currentJobId}.`;
               shouldStopPolling = true; // Dừng polling nếu lỗi server
               statusOnError = 'status_error';
           }
      } else if (error instanceof Error) {
          errorMsg = error.message;
      }

      // Chỉ đặt lỗi nếu không phải lỗi 404 khi đang polling
      if (!(axios.isAxiosError(error) && error.response?.status === 404 && pollingIntervalId)) {
          setErrorMessage(errorMsg);
      }

      if(shouldStopPolling){
          stopPolling();
          setJobStatus(statusOnError);
          // Reset thông tin chi tiết khi có lỗi nghiêm trọng
          setIsCached(null);
          setFilterMs(null);
          setOcrMs(null);
          setTranslateMs(null);
          setPdfMs(null);
      }
    } finally {
       setIsCheckingStatus(false);
    }
  }, [pollingIntervalId]);

  const startPolling = (currentJobId: string) => {
      stopPolling();
      console.log(`Starting polling for job ${currentJobId}`);
      checkStatus(currentJobId);
      // setInterval trả về number trong môi trường trình duyệt
      const intervalId = setInterval(() => {
          checkStatus(currentJobId);
      }, 5000);
      setPollingIntervalId(intervalId as unknown as number); // Ép kiểu nếu cần
  };

  const stopPolling = () => {
      if (pollingIntervalId !== null) { // Kiểm tra !== null
          console.log(`Stopping polling (interval ID: ${pollingIntervalId})`);
          clearInterval(pollingIntervalId);
          setPollingIntervalId(null);
      }
  };

  // Dọn dẹp interval khi component unmount
  useEffect(() => {
    return () => {
      stopPolling();
    };
  }, []); // Chạy một lần khi mount

  const handleDownload = () => {
    if (jobId) {
        // Mở URL download trong tab mới
        window.open(`${API_BASE_URL}/download/${jobId}`, '_blank');
    }
  };

  return (
    <div className="App">
      <h1>Image Text Processor</h1>

      <div className="upload-section">
        <input type="file" accept="image/*" onChange={handleFileChange} />
        <button onClick={handleUpload} disabled={isUploading || !selectedFile}>
          {isUploading ? 'Uploading...' : 'Upload Image'}
        </button>
      </div>

      {jobId && (
        <div className="status-section">
          <h2>Processing Status</h2>
          <p>Job ID: {jobId}</p>
          <p>Status: <strong>{jobStatus || 'N/A'}</strong> {isCheckingStatus && jobStatus !== 'completed' && jobStatus !== 'failed' && '(checking...)'}</p>
          
          {/* --- Hiển thị thông tin chi tiết khi hoàn thành --- */} 
          {jobStatus === 'completed' && (
            <div className="details">
              <p>Result Cached: {isCached === null ? 'N/A' : isCached ? 'Yes' : 'No'}</p>
              {!isCached && (
                 <> 
                  {filterMs && <p>Filtering Time: {filterMs} ms</p>} 
                  {ocrMs && <p>OCR Time: {ocrMs} ms</p>} 
                  {translateMs && <p>Translation Time: {translateMs} ms</p>} 
                  {pdfMs && <p>PDF Generation Time: {pdfMs} ms</p>} 
                 </> 
              )}
              {/* Nút download chỉ cần status completed */} 
              <button onClick={handleDownload}>Download PDF</button>
            </div>
          )}
          {/* --------------------------------------------------- */} 

        </div>
      )}

      {errorMessage && (
        <div className="error-message">
          <p>Error: {errorMessage}</p>
        </div>
      )}
    </div>
  );
}

export default App; 