#import <CoreMedia/CoreMedia.h>
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <math.h>
#import <stdio.h>
#import <stdlib.h>

static const int sampleRate = 16000;

static void fail(NSString *message) {
    fprintf(stderr, "macOS system audio: %s\n", message.UTF8String);
    exit(EXIT_FAILURE);
}

@interface AudioOutput : NSObject <SCStreamOutput, SCStreamDelegate>
@end

@implementation AudioOutput
- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
    (void)stream;
    fail(error.localizedDescription);
}

- (void)stream:(SCStream *)stream
    didOutputSampleBuffer:(CMSampleBufferRef)sample
                   ofType:(SCStreamOutputType)type {
    (void)stream;
    if (type != SCStreamOutputTypeAudio || !CMSampleBufferDataIsReady(sample)) {
        return;
    }
    const AudioStreamBasicDescription *format = CMAudioFormatDescriptionGetStreamBasicDescription(
        CMSampleBufferGetFormatDescription(sample));
    if (!format || format->mFormatID != kAudioFormatLinearPCM || format->mSampleRate != sampleRate ||
        format->mChannelsPerFrame != 1 || format->mBitsPerChannel != 32 ||
        !(format->mFormatFlags & kAudioFormatFlagIsFloat) ||
        format->mBytesPerFrame != sizeof(float)) {
        fail(@"ScreenCaptureKit did not provide 16 kHz mono float PCM");
    }

    AudioBufferList buffers = {0};
    CMBlockBufferRef block = NULL;
    OSStatus status = CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(
        sample, NULL, &buffers, sizeof(buffers), NULL, NULL, 0, &block);
    if (status != noErr || buffers.mNumberBuffers != 1) {
        fail(@"cannot read ScreenCaptureKit audio samples");
    }
    const float *input = buffers.mBuffers[0].mData;
    size_t remaining = buffers.mBuffers[0].mDataByteSize / sizeof(float);
    int16_t pcm[1024];
    while (remaining > 0) {
        size_t count = MIN(remaining, sizeof(pcm) / sizeof(pcm[0]));
        for (size_t i = 0; i < count; i++) {
            float value = input[i];
            pcm[i] = isfinite(value) ? (int16_t)lrintf(fmaxf(-32768, fminf(value * 32768, 32767))) : 0;
        }
        if (fwrite(pcm, sizeof(pcm[0]), count, stdout) != count || fflush(stdout) != 0) {
            fail(@"cannot write audio to the subtitle process");
        }
        input += count;
        remaining -= count;
    }
    if (block) {
        CFRelease(block);
    }
}
@end

static void startCapture(SCDisplay *display) {
    static AudioOutput *output;
    static SCStream *stream;

    SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display
                                                 excludingApplications:@[]
                                                      exceptingWindows:@[]];
    SCStreamConfiguration *config = [SCStreamConfiguration new];
    config.capturesAudio = YES;
    config.sampleRate = sampleRate;
    config.channelCount = 1;
    config.width = 2;
    config.height = 2;
    config.minimumFrameInterval = CMTimeMake(1, 1);
    output = [AudioOutput new];
    stream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:output];

    NSError *error = nil;
    dispatch_queue_t queue = dispatch_queue_create("audio-subtitle.capture", DISPATCH_QUEUE_SERIAL);
    if (![stream addStreamOutput:output type:SCStreamOutputTypeAudio sampleHandlerQueue:queue error:&error]) {
        fail(error.localizedDescription);
    }
    [stream startCaptureWithCompletionHandler:^(NSError *startError) {
        if (startError) {
            fail(startError.localizedDescription);
        }
    }];
}

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc > 2) {
            fail(@"expected --list-sources or a single display ID");
        }
        NSString *source = argc == 2 ? @(argv[1]) : nil;
        [SCShareableContent getShareableContentExcludingDesktopWindows:YES
                                                    onScreenWindowsOnly:YES
                                                       completionHandler:^(SCShareableContent *content, NSError *error) {
            if (error) {
                fail([NSString stringWithFormat:@"%@. Allow Screen & System Audio Recording in System Settings > Privacy & Security and relaunch.",
                                                 error.localizedDescription]);
            }
            if ([source isEqualToString:@"--list-sources"]) {
                for (SCDisplay *display in content.displays) {
                    printf("%u\t%ldx%ld\n", display.displayID, (long)display.width, (long)display.height);
                }
                exit(EXIT_SUCCESS);
            }
            SCDisplay *selected = content.displays.firstObject;
            if (source) {
                selected = nil;
                for (SCDisplay *display in content.displays) {
                    if ([source isEqualToString:[NSString stringWithFormat:@"%u", display.displayID]]) {
                        selected = display;
                        break;
                    }
                }
            }
            if (!selected) {
                fail(source ? @"display not found; run with --list-sources" : @"no active display found");
            }
            startCapture(selected);
        }];
    }
    dispatch_main();
}
