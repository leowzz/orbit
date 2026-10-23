import hashlib
import io
import json
from pathlib import Path
import uuid
import zipfile

import av
import numpy as np
import pytest

from orbit_asr.daytrace import prepare_bundle,_decode_ogg
from orbit_asr.model import decode


def ogg_audio(duration_ms):
    samples=duration_ms*16
    pcm=(np.sin(np.arange(samples)*.17)*9000).astype(np.int16)
    # A loud tail makes accidental encoder-flush truncation detectable.
    pcm[-320:]=(np.sin(np.arange(320)*.23)*14000).astype(np.int16)
    target=io.BytesIO()
    with av.open(target,mode="w",format="ogg") as container:
        stream=container.add_stream("libopus",rate=16000)
        stream.layout="mono"
        stream.codec_context.format="s16"
        stream.options={"frame_duration":"20","application":"voip"}
        for offset in range(0,samples,320):
            frame=av.AudioFrame.from_ndarray(pcm[offset:offset+320].reshape(1,-1),format="s16",layout="mono")
            frame.sample_rate=16000
            frame.pts=offset
            for packet in stream.encode(frame): container.mux(packet)
        for packet in stream.encode(None): container.mux(packet)
    return target.getvalue()


def make_bundle(path,duration=10000):
    audio=ogg_audio(duration)
    source=dict(segment_id=str(uuid.uuid4()),session_id=str(uuid.uuid4()),audio_path="audio/000000.opus",sha256=hashlib.sha256(audio).hexdigest(),
                start_at_utc_ms=1792720888912,end_at_utc_ms=1792720888912+duration,duration_ms=duration,input_sample_count=duration*16,
                input_sample_rate_hz=16000,channels=1,codec="opus")
    manifest=dict(schema="daytrace.analysis_bundle",schema_version=1,segments=[source],coverage_gaps=[])
    write_bundle(path,manifest,audio)
    return manifest,audio


def write_bundle(path,manifest,audio,compression=zipfile.ZIP_STORED):
    with zipfile.ZipFile(path,"w",compression=compression) as archive:
        archive.writestr("manifest.json",json.dumps(manifest))
        archive.writestr("audio/000000.opus",audio)


@pytest.mark.parametrize("duration,expected_chunks",[(1000,1),(10000,2),(30000,4)])
def test_ogg_preskip_balanced_chunks_preserve_complete_tail(tmp_path,duration,expected_chunks):
    path=tmp_path/"bundle.zip"
    manifest,audio=make_bundle(path,duration)
    source_pcm=_decode_ogg(audio,duration*16)
    assert len(source_pcm)==duration*16  # Ogg preskip+EOS trimming applied.
    summary,items=prepare_bundle(path,"daytrace-test")
    assert len(items)==expected_chunks
    assert summary["source_duration_ms"]==duration
    assert summary["wire_duration_ms"]>duration
    assert sum(i["source"]["valid_duration_ms"] for i in items)==duration
    durations=[i["source"]["valid_duration_ms"] for i in items]
    assert max(durations)-min(durations)<=20
    offset=0
    for item in items:
        ledger=item["source"]
        segment=item["segment"]
        assert ledger["offset_ms"]==offset//16
        assert segment["duration_ms"]<=10000
        assert segment["session_id"]==manifest["segments"][0]["session_id"]
        restored=decode(segment)
        delay=ledger["encoder_delay_samples"]
        count=ledger["valid_duration_ms"]*16
        valid=restored[delay:delay+count]
        assert len(valid)==count
        # Different lossy encoders need not reproduce exact PCM; aligned content
        # must nevertheless correlate, including the source's final 20 ms tail.
        assert np.corrcoef(valid,source_pcm[offset:offset+count])[0,1]>.9
        assert len(restored)==delay+count+ledger["tail_padding_samples"]
        offset+=count
    assert np.max(np.abs(valid[-320:]))>.05
    assert offset==duration*16


def test_ids_stable_across_repack_and_different_for_node(tmp_path):
    path=tmp_path/"original.zip"
    manifest,audio=make_bundle(path,1000)
    repack=tmp_path/"repacked.zip"
    write_bundle(repack,manifest,audio,zipfile.ZIP_DEFLATED)
    first,items=prepare_bundle(path,"one")
    second,repacked=prepare_bundle(repack,"one")
    assert first["bundle_sha256"]!=second["bundle_sha256"]
    assert items==repacked
    _,other=prepare_bundle(path,"two")
    assert items[0]["segment"]["segment_id"]!=other[0]["segment"]["segment_id"]


@pytest.mark.parametrize("failure",["checksum","samples","path","duration"])
def test_invalid_source_fails_whole_bundle(tmp_path,failure):
    path=tmp_path/"bad.zip"
    manifest,audio=make_bundle(path,1000)
    source=manifest["segments"][0]
    if failure=="checksum": source["sha256"]="0"*64
    elif failure=="samples": source["input_sample_count"]-=1
    elif failure=="path": source["audio_path"]="../audio.opus"
    elif failure=="duration": source["duration_ms"]=30020
    write_bundle(path,manifest,audio)
    with pytest.raises(ValueError): prepare_bundle(path,"test")


def test_source_payload_unchanged_for_subset_or_added_earlier_source(tmp_path):
    original=tmp_path/"original.zip"
    manifest,audio=make_bundle(original,1000)
    _,baseline=prepare_bundle(original,"same-node")
    extra=dict(manifest["segments"][0])
    extra.update(segment_id=str(uuid.uuid4()),audio_path="audio/000001.opus",
                 start_at_utc_ms=manifest["segments"][0]["start_at_utc_ms"]-2000,
                 end_at_utc_ms=manifest["segments"][0]["end_at_utc_ms"]-2000)
    expanded=dict(manifest,segments=[extra,*manifest["segments"]])
    path=tmp_path/"expanded.zip"
    with zipfile.ZipFile(path,"w") as archive:
        archive.writestr("manifest.json",json.dumps(expanded))
        archive.writestr("audio/000000.opus",audio)
        archive.writestr("audio/000001.opus",audio)
    _,all_items=prepare_bundle(path,"same-node")
    existing=[item for item in all_items if item["source"]["segment_id"]==manifest["segments"][0]["segment_id"]]
    assert existing==baseline
    assert json.dumps(existing,sort_keys=True)==json.dumps(baseline,sort_keys=True)
    assert all(0<=item["segment"]["sequence"]<=2**63-1 for item in all_items)
    # Removing the earlier source reproduces exactly the original job bytes.
    subset=tmp_path/"subset.zip"
    write_bundle(subset,dict(expanded,segments=manifest["segments"]),audio)
    _,subset_items=prepare_bundle(subset,"same-node")
    assert subset_items==existing
