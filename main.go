package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var wg sync.WaitGroup

func main() {
	sdkConfig, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Println("Couldn't load default configuration. Have you set up your AWS account?")
		log.Println(err)
		return
	}
	s3Client := s3.NewFromConfig(sdkConfig, func(o *s3.Options) { o.Region = "us-east-1" })

	for {
		awsS3Bucket := aws.String("infra-binaries")
		awsS3Prefix := aws.String("entrypoint")
		s3Files, err := s3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{Bucket: awsS3Bucket, Prefix: awsS3Prefix})
		if err != nil {
			log.Println("Couldn't get ListObjectsV2!")
			log.Println(err)
			return
		}

		for _, sa := range s3Files.Contents {
			if strings.HasSuffix(*sa.Key, "/") {
				// Skip directory like object
				continue
			}

			fileName := filepath.Base(*sa.Key)
			log.Println(fileName)
			fsInfo, err := os.Stat(fileName)
			if err == nil {
				if fsInfo.Size() != sa.Size || fsInfo.ModTime().Before(*sa.LastModified) {
					wg.Add(1)
					go downloadFile(s3Client, awsS3Bucket, sa.Key, fileName)
					log.Println("Need update")
				}
			} else if errors.Is(err, fs.ErrNotExist) {
				log.Println("Need create")
				wg.Add(1)
				go downloadFile(s3Client, awsS3Bucket, sa.Key, fileName)
			} else {
				log.Println("Error checking file Stat")
				log.Println(err)
				return
			}
		}
		wg.Wait()
		log.Println("Done")

		time.Sleep(time.Duration(5 * time.Minute))

	}
}

// DownloadFile gets an object from a bucket and stores it in a local file.
func downloadFile(s3Client *s3.Client, bucketName *string, objectKey *string, fileName string) error {
	defer wg.Done()

	result, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: bucketName,
		Key:    objectKey,
	})
	if err != nil {
		log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		return err
	}
	defer result.Body.Close()
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("Couldn't create file %v. Here's why: %v\n", fileName, err)
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	if err != nil {
		log.Printf("Couldn't write file %v. Here's why: %v\n", fileName, err)
		return err
	}
	if !strings.HasSuffix(fileName, ".exe") && runtime.GOOS != "windows" {
		err := os.Chmod(fileName, 0777)
		if err != nil {
			log.Println("Couldn Chmod")
			log.Println(err)
			return err
		}
	}
	err = os.Chtimes(fileName, *result.LastModified, *result.LastModified)
	if err != nil {
		log.Println("Couldn Chtimes")
		log.Println(err)
		return err
	}
	return nil
}
